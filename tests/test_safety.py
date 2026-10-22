# Ryowatch - tests/test_safety.py
# Pure unit tests for the safety nets. No RPC, no network.

import time
import pytest
from src import safety


def test_gas_check_passes_when_under_limit():
    # We can't easily mock w3 without a real connection, so test the logic directly.
    sn = safety.SafetyNet()
    # MAX_GAS_PRICE_GWEI default is 50; with a fake low gas price it should pass.
    # We'll monkeypatch by calling the method with a fake w3 object.
    class FakeW3:
        class eth:
            gas_price = 10 * 10**9  # 10 gwei
    ok, reason = sn.check_gas(FakeW3())
    assert ok is True
    assert reason == "ok"


def test_gas_check_fails_when_over_limit(monkeypatch):
    import src.config as cfg
    monkeypatch.setattr(cfg, "MAX_GAS_PRICE_GWEI", 5)
    sn = safety.SafetyNet()
    class FakeW3:
        class eth:
            gas_price = 10 * 10**9  # 10 gwei > 5 gwei limit
    ok, reason = sn.check_gas(FakeW3())
    assert ok is False
    assert "gas too high" in reason


def test_gas_check_disabled_when_zero(monkeypatch):
    import src.config as cfg
    monkeypatch.setattr(cfg, "MAX_GAS_PRICE_GWEI", 0)
    sn = safety.SafetyNet()
    class FakeW3:
        class eth:
            gas_price = 999 * 10**9  # absurdly high
    ok, reason = sn.check_gas(FakeW3())
    assert ok is True


def test_trade_size_check_passes():
    sn = safety.SafetyNet()
    ok, reason = sn.check_trade_size(0.5, "0xabc")
    assert ok is True


def test_trade_size_check_fails(monkeypatch):
    import src.config as cfg
    monkeypatch.setattr(cfg, "MAX_TRADE_SIZE", 0.5)
    sn = safety.SafetyNet()
    ok, reason = sn.check_trade_size(1.0, "0xabc")
    assert ok is False
    assert "trade too large" in reason


def test_trade_size_check_disabled_when_zero(monkeypatch):
    import src.config as cfg
    monkeypatch.setattr(cfg, "MAX_TRADE_SIZE", 0)
    sn = safety.SafetyNet()
    ok, reason = sn.check_trade_size(999999.0, "0xabc")
    assert ok is True


def test_rate_limit_initially_ok():
    sn = safety.SafetyNet()
    ok, reason = sn.check_rate_limit()
    assert ok is True


def test_rate_limit_blocks_rapid_calls():
    sn = safety.SafetyNet()
    sn.last_trade_time = time.time()  # just traded
    ok, reason = sn.check_rate_limit()
    assert ok is False
    assert "rate limited" in reason


def test_circuit_breaker_closed_initially():
    sn = safety.SafetyNet()
    ok, reason = sn.check_circuit_breaker()
    assert ok is True


def test_circuit_breaker_opens_after_failures(monkeypatch):
    import src.config as cfg
    monkeypatch.setattr(cfg, "CIRCUIT_BREAKER_FAILURES", 2)
    sn = safety.SafetyNet()
    sn.record_failure()
    sn.record_failure()
    ok, reason = sn.check_circuit_breaker()
    assert ok is False
    assert "circuit breaker open" in reason


def test_circuit_breaker_resets_on_success(monkeypatch):
    import src.config as cfg
    monkeypatch.setattr(cfg, "CIRCUIT_BREAKER_FAILURES", 2)
    sn = safety.SafetyNet()
    sn.record_failure()
    sn.record_failure()
    sn.record_success()
    ok, reason = sn.check_circuit_breaker()
    assert ok is True
    assert sn.consecutive_failures == 0


def test_all_clear_combines_all_checks():
    sn = safety.SafetyNet()
    class FakeW3:
        class eth:
            gas_price = 10 * 10**9
    ok, reason = sn.all_clear(FakeW3(), 0.5, "0xabc")
    assert ok is True
    assert reason == "ok"


def test_record_success_updates_timestamp():
    sn = safety.SafetyNet()
    before = sn.last_trade_time
    time.sleep(0.01)
    sn.record_success()
    assert sn.last_trade_time > before
    assert sn.consecutive_failures == 0


def test_record_failure_increments():
    sn = safety.SafetyNet()
    sn.record_failure()
    sn.record_failure()
    assert sn.consecutive_failures == 2
