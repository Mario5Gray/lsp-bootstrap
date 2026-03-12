"""
Multi-file fixture — calls Worker.run.

Known facts for test assertions:
  - dispatch() calls Worker.run() at the line marked REFERENCE
"""
from __future__ import annotations

from worker import Worker


def dispatch(worker_id: int) -> str:
    w = Worker(worker_id)
    return w.run()  # REFERENCE — line 14
