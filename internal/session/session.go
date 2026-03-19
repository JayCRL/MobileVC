package session

type State string

const (
	StateActive      State = "active"
	StateHibernating State = "hibernating"
	StateClosed      State = "closed"
)

type Session struct {
	ID    string
	State State
}
