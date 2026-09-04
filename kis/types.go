package kis

// Mode selects a documented KIS transaction-ID column. Host selection remains
// independent: callers must still explicitly configure HostVTS or HostLive.
type Mode string

const (
	Mock Mode = "mock"
	Live Mode = "live"
)

func TransactionID(mode Mode, mock, live string) (string, error) {
	switch mode {
	case Mock:
		return mock, nil
	case Live:
		return live, nil
	default:
		return "", ErrInvalidMode
	}
}
