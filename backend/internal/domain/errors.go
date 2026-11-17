package domain

import "errors"

// ErrFIXNotTransmitted marks an adapter error returned before anything was
// written to the FIX session, e.g. the session dropped between routing and
// send. Such a send is safe to retry; any other send error is an unknown wire
// outcome that needs reconciliation.
var ErrFIXNotTransmitted = errors.New("FIX message not transmitted")
