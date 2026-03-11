"""
Acceptance test fixture — Python with a deliberate type error.

Known facts for test assertions:
  - Line 14: type error — assigning int return value to str variable
  - Expected diagnostic: severity "error", points to line 14
"""
from __future__ import annotations


def add(x: int, y: int) -> int:
    return x + y


result: str = add(1, 2)  # ERROR: expression of type "int" cannot be assigned to "str"
