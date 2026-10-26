// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package ttl

import "time"

// TTLMarker marks an elastic-agent install kept on disk until ValidUntil: an available rollback target,
// or an install retained only for its logs (see LogsOnly)
type TTLMarker struct {
	Version    string    `json:"version" yaml:"version"`
	Hash       string    `json:"hash" yaml:"hash"`
	ValidUntil time.Time `json:"valid_until" yaml:"valid_until"`
	// LogsOnly marks an install that is kept on disk only to retain its logs until ValidUntil
	// (e.g. the home of an upgrade that failed and was rolled back). It is protected from cleanup like
	// any unexpired entry but it is not a usable rollback target.
	// The zero value means a regular rollback target, so markers written by older agents stay valid.
	LogsOnly bool `json:"logs_only,omitempty" yaml:"logs_only,omitempty"`
}
