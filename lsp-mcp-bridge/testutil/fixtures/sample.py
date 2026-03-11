"""
Acceptance test fixture — Python.

Known facts for test assertions:
  - get_result() return type : Future[tuple[bytes, int]]
  - process() parameter `fut`: Future[tuple[bytes, int]]
  - Worker.submit() is defined at the line marked DEFINITION
  - submit() is called at the line marked REFERENCE
"""
from __future__ import annotations

from concurrent.futures import Future


class Worker:
    def submit(self, payload: bytes) -> Future[tuple[bytes, int]]:  # DEFINITION
        """Submit payload and return a future resolving to (result, code)."""
        raise NotImplementedError


def get_result() -> Future[tuple[bytes, int]]:
    w = Worker()
    return w.submit(b"ping")  # REFERENCE


def process(fut: Future[tuple[bytes, int]]) -> bytes:
    result = fut.result()  # hover here → Future[tuple[bytes, int]]
    value, _code = result
    return value
