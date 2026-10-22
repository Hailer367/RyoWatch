# Ryowatch entry point.
# Usage: python run.py

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from src import config
from src import watcher

if __name__ == "__main__":
    watcher.run()
