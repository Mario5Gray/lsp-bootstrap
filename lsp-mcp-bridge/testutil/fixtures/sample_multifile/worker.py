"""
Multi-file fixture — worker definition.

Known facts for test assertions:
  - Worker.run() is defined at the line marked DEFINITION
  - worker_id parameter is on the __init__ line
"""
from __future__ import annotations


class Worker:
    def __init__(self, worker_id: int) -> None:
        self.worker_id = worker_id

    def run(self) -> str:  # DEFINITION — line 15
        return f"worker-{self.worker_id}"
