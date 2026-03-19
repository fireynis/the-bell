# Beads Parent-Child Dependency Fix

**Date**: 2026-03-01

## Problem
When tasks are created as children of epics (`bd create --parent=<epic>`), beads
creates a `parent-child` dependency that `bd ready` treats as a blocker. Since epics
remain open until all children are closed, this creates a circular deadlock — children
can't become ready because the parent is open, and the parent can't close because
children are open.

## Fix Applied
Removed all `parent-child` dependencies from all 38 child tasks across all 7 epics.
Tasks now rely only on their explicit `blocks` dependencies for ordering.

## Future Prevention
When creating new child tasks, either:
1. Don't use `--parent=<epic>` (use explicit `bd dep add` for real blockers only)
2. If using `--parent`, immediately remove the generated parent-child dep:
   `bd dep remove <child> <epic>`
