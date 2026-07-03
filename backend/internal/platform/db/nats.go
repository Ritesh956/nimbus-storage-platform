package db

import (
	"fmt"

	"github.com/nats-io/nats.go"
)

// NewNATS connects to the NATS server. The connection itself proves
// reachability (nats.go dials synchronously by default).
func NewNATS(url string) (*nats.Conn, error) {
	conn, err := nats.Connect(url, nats.Name("nimbus"))
	if err != nil {
		return nil, fmt.Errorf("nats: %w", err)
	}
	return conn, nil
}
