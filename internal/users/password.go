package users

import (
	"errors"
	"fmt"
)

// MinPasswordLen is the shared floor for onboard, register, self-service
// change, and admin reset. Shorter values used to slip through onboard
// and /api/me/password while /api/register rejected them.
const MinPasswordLen = 8

// CheckPasswordLength rejects empty and too-short passwords. Length is
// in bytes, matching the existing register handler.
func CheckPasswordLength(password string) error {
	if password == "" {
		return errors.New("password required")
	}
	if len(password) < MinPasswordLen {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLen)
	}
	return nil
}
