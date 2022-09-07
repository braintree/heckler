package snow

type SNowManager interface {
	GetChangeTicket(changeID string) (string, error)
}
