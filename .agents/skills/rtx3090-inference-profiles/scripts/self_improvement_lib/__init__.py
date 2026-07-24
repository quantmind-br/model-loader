"""Runtime-neutral self-improvement control plane shared by skills.

This package is the single policy implementation for the learning-candidate,
evaluation, approval, promotion, dual-copy synchronization, and rollback loop.
Per-skill wrappers are argument/config adapters only; all gates live in core.
"""
from __future__ import annotations

from . import core

__all__ = ["core"]
