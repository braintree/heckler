package heckler

import (
	"fmt"
	"log"

	"github.com/braintree/heckler/internal/rizzopb"
)

type LockStatus int

const (
	LockUnknown LockStatus = iota
	LockedByUser
	LockedByAnother
	Unlocked
)

type LockState struct {
	LockStatus
	User    string
	Comment string
}

func (ls LockState) String() string {
	return fmt.Sprintf("LockState, User: '%s' Comment: '%s'", ls.User, ls.Comment)
}

func LockReportToLockState(lr rizzopb.PuppetLockReport) LockState {
	var ls LockState
	switch lr.LockStatus {
	case rizzopb.LockStatus_lock_unknown:
		ls.LockStatus = LockUnknown
	case rizzopb.LockStatus_locked_by_user:
		ls.LockStatus = LockedByUser
	case rizzopb.LockStatus_locked_by_another:
		ls.LockStatus = LockedByAnother
	case rizzopb.LockStatus_unlocked:
		ls.LockStatus = Unlocked
	default:
		log.Fatal("Unknown lockStatus!")
	}
	ls.Comment = lr.Comment
	ls.User = lr.User
	return ls
}
