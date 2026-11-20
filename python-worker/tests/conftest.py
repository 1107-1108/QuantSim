import sys
from pathlib import Path

# python-worker/ 加入 import 路径,使 tests 能 import protocol
sys.path.insert(0, str(Path(__file__).resolve().parent.parent))
