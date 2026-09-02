package kis

import "errors"

var ErrInvalidMode = errors.New("kis: mode must be kis.Mock or kis.Live")
