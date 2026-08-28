package backup

import (
	"context"
	"fmt"
)

// wipeDatabase drops public schema. Call only after decrypt + verify succeed.
func (s *Service) wipeDatabase(ctx context.Context) error {
	if s.WipeFn != nil {
		s.wiped = true
		return s.WipeFn(ctx)
	}
	if s.pool == nil {
		return fmt.Errorf("database pool is not configured")
	}
	s.wiped = true
	_, err := s.pool.Exec(ctx, `
		DROP SCHEMA IF EXISTS public CASCADE;
		CREATE SCHEMA public;
		GRANT ALL ON SCHEMA public TO CURRENT_USER;
		GRANT ALL ON SCHEMA public TO public;
	`)
	if err != nil {
		return fmt.Errorf("wipe failed: %w", err)
	}
	return nil
}

// Wiped reports whether wipeDatabase ran (tests).
func (s *Service) Wiped() bool { return s.wiped }
