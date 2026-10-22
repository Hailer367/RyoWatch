# Ryowatch - safety.py
# The bot's safety nets. The contract trusts the bot, so these must be solid.

import time
from . import config


class SafetyNet:
    def __init__(self):
        self.consecutive_failures = 0
        self.last_trade_time = 0.0
        self.min_trade_interval = 1.0  # seconds between trades

    def check_gas(self, w3):
        """Refuse to trade when gas is too expensive."""
        gas_price_gwei = w3.eth.gas_price / 1e9
        if config.MAX_GAS_PRICE_GWEI > 0 and gas_price_gwei > config.MAX_GAS_PRICE_GWEI:
            return False, f"gas too high: {gas_price_gwei} > {config.MAX_GAS_PRICE_GWEI} gwei"
        return True, "ok"

    def check_trade_size(self, amount, asset):
        """Refuse trades larger than the configured cap."""
        if config.MAX_TRADE_SIZE > 0 and amount > config.MAX_TRADE_SIZE:
            return False, f"trade too large: {amount} > {config.MAX_TRADE_SIZE}"
        return True, "ok"

    def check_rate_limit(self):
        """Don't spam the chain."""
        now = time.time()
        if now - self.last_trade_time < self.min_trade_interval:
            return False, "rate limited"
        return True, "ok"

    def check_circuit_breaker(self):
        """Pause after too many consecutive failures."""
        if self.consecutive_failures >= config.CIRCUIT_BREAKER_FAILURES:
            return False, f"circuit breaker open ({self.consecutive_failures} failures)"
        return True, "ok"

    def record_success(self):
        self.consecutive_failures = 0
        self.last_trade_time = time.time()

    def record_failure(self):
        self.consecutive_failures += 1
        self.last_trade_time = time.time()

    def all_clear(self, w3, amount, asset):
        """Run every check; return (ok, reason)."""
        for check in (self.check_gas(w3), self.check_trade_size(amount, asset), self.check_rate_limit(), self.check_circuit_breaker()):
            if not check[0]:
                return False, check[1]
        return True, "ok"
