package heckler

import (
	"log"

	"github.braintreeps.com/lollipopman/heckler/internal/rizzopb"
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
