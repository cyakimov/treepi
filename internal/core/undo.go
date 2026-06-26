package core

import "context"

// UndoResult reports what undo reversed.
type UndoResult struct {
	Reverted bool   `json:"reverted"`
	Op       string `json:"op,omitempty"`
	OpID     string `json:"op_id,omitempty"`
}

// Undo reverses the last committed operation repo-wide (refs, worktrees, and the
// manifest), refusing to rewind a published trunk.
func (s *Service) Undo(ctx context.Context) (*UndoResult, error) {
	op, err := s.store.Undo(ctx, s.executor())
	if err != nil {
		return nil, err
	}
	if op == nil {
		return &UndoResult{Reverted: false}, nil
	}
	return &UndoResult{Reverted: true, Op: op.Kind, OpID: op.ID}, nil
}
