// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package upgrade

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent/internal/pkg/agent/application/paths"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/upgrade/ttl"
	"github.com/elastic/elastic-agent/pkg/control/v2/client"
	"github.com/elastic/elastic-agent/pkg/core/logger"
	"github.com/elastic/elastic-agent/pkg/core/logger/loggertest"
	"github.com/elastic/elastic-agent/pkg/upgrade/details"
)

// setupRolledBackAgentDirs creates the state the restarted (rolled back) agent finds on disk after a
// watcher rollback: the previous home, the failed home with a log file and an upgrade marker in UPG_ROLLBACK.
// It returns the previous home, the failed home and the path of the failed home log file.
func setupRolledBackAgentDirs(t *testing.T, topDir string) (prev, failed, failedLog string) {
	t.Helper()
	prev = createFakeAgentInstall(t, topDir, "9.5.3", "0e70ce", true)
	failed = createFakeAgentInstall(t, topDir, "9.5.4", "b18dae", true)
	failedLog = filepath.Join(topDir, failed, "logs", "elastic-agent-watcher.ndjson")
	require.NoError(t, os.WriteFile(failedLog, []byte("Error detected, proceeding to rollback"), 0o644))

	require.NoError(t, SaveMarker(paths.DataFrom(topDir), &UpdateMarker{
		Version: "9.5.4", Hash: "b18dae", VersionedHome: failed,
		PrevVersion: "9.5.3", PrevHash: "0e70ce", PrevVersionedHome: prev,
		Details: &details.Details{State: details.StateRollback},
	}, false))
	return prev, failed, failedLog
}

// startupCleanup runs what application.normalizeAgentInstalls runs when the agent starts.
func startupCleanup(t *testing.T, topDir, currentHome string) error {
	t.Helper()
	log, _ := loggertest.New(t.Name())
	marker, err := LoadMarker(paths.DataFrom(topDir))
	require.NoError(t, err)
	_, err = CleanAvailableRollbacks(log, ttl.NewTTLMarkerRegistry(log, topDir), topDir, currentHome, time.Now(),
		PreserveActiveUpgradeVersions(marker, CleanupExpiredRollbacks))
	return err
}

// Regression test for #17234: the startup cleanup of the rolled back agent used to delete the failed home,
// logs included, before the rollback cleanup could preserve them.
func TestRetainFailedUpgradeLogs_SurvivesStartupCleanup(t *testing.T) {
	log, _ := loggertest.New(t.Name())

	t.Run("without retention marker the failed home is removed", func(t *testing.T) {
		topDir := t.TempDir()
		prev, failed, failedLog := setupRolledBackAgentDirs(t, topDir)
		require.NoError(t, startupCleanup(t, topDir, prev))
		assert.NoFileExists(t, failedLog)
		assert.NoDirExists(t, filepath.Join(topDir, failed))
	})

	t.Run("with retention marker the failed home survives", func(t *testing.T) {
		topDir := t.TempDir()
		prev, failed, failedLog := setupRolledBackAgentDirs(t, topDir)
		require.NoError(t, RetainFailedUpgradeLogs(log, topDir, failed, "9.5.4", "b18dae", time.Now().Add(time.Hour)))

		require.NoError(t, startupCleanup(t, topDir, prev))
		assert.FileExists(t, failedLog, "logs of the failed upgrade must survive the startup cleanup")
	})

	t.Run("expired retention marker: the failed home is removed whole", func(t *testing.T) {
		topDir := t.TempDir()
		prev, failed, failedLog := setupRolledBackAgentDirs(t, topDir)
		require.NoError(t, RetainFailedUpgradeLogs(log, topDir, failed, "9.5.4", "b18dae", time.Now().Add(-time.Hour)))

		require.NoError(t, startupCleanup(t, topDir, prev))
		assert.NoFileExists(t, failedLog)
		assert.NoDirExists(t, filepath.Join(topDir, failed))
	})
}

func TestRetainFailedUpgradeLogs_KeepsExistingMarkers(t *testing.T) {
	log, _ := loggertest.New(t.Name())
	topDir := t.TempDir()
	prev, failed, _ := setupRolledBackAgentDirs(t, topDir)
	registry := ttl.NewTTLMarkerRegistry(log, topDir)
	prevTTL := ttl.TTLMarker{Version: "9.5.3", Hash: "0e70ce", ValidUntil: time.Now().Add(24 * time.Hour)}
	require.NoError(t, registry.Set(map[string]ttl.TTLMarker{prev: prevTTL}))

	validUntil := time.Now().Add(time.Hour)
	require.NoError(t, RetainFailedUpgradeLogs(log, topDir, failed, "9.5.4", "b18dae", validUntil))

	got, malformed, err := registry.GetAll()
	require.NoError(t, err)
	assert.Empty(t, malformed)
	require.Len(t, got, 2)
	assert.Equal(t, prevTTL.Version, got[prev].Version)
	assert.False(t, got[prev].LogsOnly)
	assert.Equal(t, "9.5.4", got[failed].Version)
	assert.Equal(t, "b18dae", got[failed].Hash)
	assert.True(t, got[failed].LogsOnly)
	assert.WithinDuration(t, validUntil, got[failed].ValidUntil, time.Second)
}

func TestRetainFailedUpgradeLogs_NoHome(t *testing.T) {
	log, _ := loggertest.New(t.Name())
	assert.Error(t, RetainFailedUpgradeLogs(log, t.TempDir(), "", "9.5.4", "b18dae", time.Now()))
}

func TestFailedUpgradeHome(t *testing.T) {
	assert.Equal(t, "", FailedUpgradeHome(nil))
	assert.Equal(t, filepath.Join("data", "elastic-agent-9.5.4-b18dae"),
		FailedUpgradeHome(&UpdateMarker{Hash: "b18dae2c72", VersionedHome: "data/elastic-agent-9.5.4-b18dae"}))
	assert.Equal(t, filepath.Join("data", "elastic-agent-b18dae"),
		FailedUpgradeHome(&UpdateMarker{Hash: "b18dae2c72"}), "marker of older agents has no versioned home")
	assert.Equal(t, "", FailedUpgradeHome(&UpdateMarker{Hash: "b1"}))
}

func TestRollbackTargets(t *testing.T) {
	log, _ := loggertest.New(t.Name())
	topDir := t.TempDir()
	valid := time.Now().Add(time.Hour)

	regular := createFakeAgentInstall(t, topDir, "9.5.3", "0e70ce", true)
	flagged := createFakeAgentInstall(t, topDir, "9.5.4", "b18dae", true)
	// marker rewritten by an agent that does not know the flag: only the disk state tells it is not a target
	stripped := createFakeAgentInstall(t, topDir, "9.5.5", "f97178", true)
	require.NoError(t, os.RemoveAll(filepath.Join(topDir, stripped, "components")))
	require.NoError(t, os.RemoveAll(filepath.Join(topDir, stripped, "run")))
	require.NoError(t, os.Remove(paths.BinaryPath(filepath.Join(topDir, stripped), AgentName)))

	rollbacks := map[string]ttl.TTLMarker{
		regular:  {Version: "9.5.3", Hash: "0e70ce", ValidUntil: valid},
		flagged:  {Version: "9.5.4", Hash: "b18dae", ValidUntil: valid, LogsOnly: true},
		stripped: {Version: "9.5.5", Hash: "f97178", ValidUntil: valid},
	}

	targets := RollbackTargets(log, topDir, rollbacks)
	assert.Equal(t, map[string]ttl.TTLMarker{regular: rollbacks[regular]}, targets)

	_, _, found := findRollbackTarget(targets, time.Now(), "9.5.4")
	assert.False(t, found, "an install kept only for its logs must not be a rollback target")
	_, _, found = findRollbackTarget(targets, time.Now(), "9.5.5")
	assert.False(t, found, "a log-only install must not be a rollback target")
	home, _, found := findRollbackTarget(targets, time.Now(), "9.5.3")
	assert.True(t, found)
	assert.Equal(t, regular, home)
}

func TestRollbackWithOpts_StripsRetainedHome(t *testing.T) {
	setup := func(t *testing.T) (log *logger.Logger, topDir string, mockClient *client.MockClient) {
		t.Helper()
		log, _ = loggertest.New(t.Name())
		topDir = t.TempDir()
		setupAgents(t, log, topDir, setupAgentInstallations{
			installedAgents: []testAgentInstall{
				{version: version123Snapshot, useVersionInPath: true},
				{version: version456Snapshot, useVersionInPath: true},
			},
			upgradeFrom:  version123Snapshot,
			upgradeTo:    version456Snapshot,
			currentAgent: version456Snapshot,
		}, true)
		mockClient = client.NewMockClient(t)
		mockClient.EXPECT().Connect(mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mockClient.EXPECT().Disconnect().Return()
		mockClient.EXPECT().Restart(mock.Anything).Return(nil).Once()
		return log, topDir, mockClient
	}
	prevHome := "data/elastic-agent-1.2.3-SNAPSHOT-abcdef"
	failedHome := filepath.Join("data", "elastic-agent-4.5.6-SNAPSHOT-ghijkl")

	t.Run("retained home is stripped to logs and TTL marker", func(t *testing.T) {
		log, topDir, mockClient := setup(t)
		require.NoError(t, RetainFailedUpgradeLogs(log, topDir, failedHome, "4.5.6-SNAPSHOT", "ghijkl", time.Now().Add(time.Hour)))

		err := RollbackWithOpts(t.Context(), log, mockClient, topDir, prevHome, "abcdef", func(rs *RollbackSettings) {
			rs.SetRetainedHome(failedHome)
		})
		require.NoError(t, err)

		assert.DirExists(t, filepath.Join(topDir, failedHome, "logs"))
		assert.FileExists(t, filepath.Join(topDir, failedHome, ttl.MarkerFileName))
		assert.NoDirExists(t, filepath.Join(topDir, failedHome, "components"))
		assert.NoDirExists(t, filepath.Join(topDir, failedHome, "run"))
		assert.NoFileExists(t, paths.BinaryPath(filepath.Join(topDir, failedHome), AgentName))
		assert.True(t, hasOnlyLogs(log, filepath.Join(topDir, failedHome)))
		assertAgentInstallExists(t, filepath.Join(topDir, prevHome), AgentName)
	})

	t.Run("the home rolled back to is never stripped", func(t *testing.T) {
		log, topDir, mockClient := setup(t)

		err := RollbackWithOpts(t.Context(), log, mockClient, topDir, prevHome, "abcdef", func(rs *RollbackSettings) {
			rs.SetRetainedHome(prevHome)
		})
		require.NoError(t, err)
		assertAgentInstallExists(t, filepath.Join(topDir, prevHome), AgentName)
	})
}
