// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package upgrade

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/elastic/elastic-agent/internal/pkg/agent/application/upgrade/ttl"
	"github.com/elastic/elastic-agent/pkg/core/logger"
)

// FailedUpgradeHome returns the versioned home, relative to the top path, of the version a failed upgrade
// was upgrading to, as recorded in the upgrade marker.
func FailedUpgradeHome(marker *UpdateMarker) string {
	if marker == nil {
		return ""
	}
	if marker.VersionedHome != "" {
		return filepath.Clean(marker.VersionedHome)
	}
	// the marker may have been written by an older agent where the versioned home is always data/elastic-agent-<shortHash>
	if len(marker.Hash) >= HashLen {
		return filepath.Join("data", fmt.Sprintf("%s-%s", AgentName, marker.Hash[:HashLen]))
	}
	return ""
}

// RetainFailedUpgradeLogs writes a TTL marker flagged LogsOnly into the versioned home of a failed upgrade.
// Any agent cleanup keeps homes with an unexpired TTL marker, including the cleanup the rolled back agent runs
// when it starts, which would otherwise delete the home (and its logs) before the rollback cleanup runs.
// It must be called before the agent is restarted. Existing TTL markers are not touched.
func RetainFailedUpgradeLogs(log *logger.Logger, topDir string, failedHome string, version, hash string, validUntil time.Time) error {
	if failedHome == "" || failedHome == "." {
		return fmt.Errorf("no versioned home to retain logs of")
	}

	marker := ttl.TTLMarker{
		Version:    version,
		Hash:       hash,
		ValidUntil: validUntil,
		LogsOnly:   true,
	}
	// write only this marker: rewriting the others would risk corrupting a real rollback target's marker
	if err := ttl.NewTTLMarkerRegistry(log, topDir).Add(failedHome, marker); err != nil {
		return fmt.Errorf("writing logs retention TTL marker for %q: %w", failedHome, err)
	}
	return nil
}
