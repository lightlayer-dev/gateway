package cli

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lightlayer-dev/gateway/configs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncBuffer is a thread-safe bytes.Buffer for use in concurrent tests.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (sb *syncBuffer) Write(p []byte) (int, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Write(p)
}

func (sb *syncBuffer) String() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.String()
}

func TestInitCreatesFile(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { os.Chdir(orig) })

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"init"})
	require.NoError(t, rootCmd.Execute())

	data, err := os.ReadFile(filepath.Join(dir, "gateway.yaml"))
	require.NoError(t, err)
	assert.Equal(t, configs.GatewayYAML, data)
	assert.Contains(t, buf.String(), "Created gateway.yaml")
}

func TestInitRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { os.Chdir(orig) })

	require.NoError(t, os.WriteFile("gateway.yaml", []byte("existing"), 0644))

	rootCmd.SetArgs([]string{"init"})
	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestInitForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { os.Chdir(orig) })

	require.NoError(t, os.WriteFile("gateway.yaml", []byte("old"), 0644))

	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"init", "--force"})
	require.NoError(t, rootCmd.Execute())

	data, err := os.ReadFile("gateway.yaml")
	require.NoError(t, err)
	assert.Equal(t, configs.GatewayYAML, data)
}

func TestValidateCatchesErrors(t *testing.T) {
	dir := t.TempDir()
	badCfg := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(badCfg, []byte("gateway:\n  origin:\n    url: \"\"\n"), 0644))

	rootCmd.SetArgs([]string{"validate", "--config", badCfg})
	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid config")
}

func TestValidateAcceptsGoodConfig(t *testing.T) {
	dir := t.TempDir()
	goodCfg := filepath.Join(dir, "good.yaml")
	require.NoError(t, os.WriteFile(goodCfg, configs.GatewayYAML, 0644))

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"validate", "--config", goodCfg})
	require.NoError(t, rootCmd.Execute())
	assert.Contains(t, buf.String(), "Config is valid")
}

func TestStartBootsServer(t *testing.T) {
	// Pick a free port for the proxy so tests don't collide.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	dir := t.TempDir()
	cfgContent := []byte(fmt.Sprintf(`
gateway:
  listen:
    port: %d
    host: 127.0.0.1
  origin:
    url: https://httpbin.org
    timeout: 5s
plugins:
  discovery:
    enabled: false
  payments:
    enabled: false
  analytics:
    enabled: false
admin:
  enabled: false
  port: 19091
`, port))
	cfgPath := filepath.Join(dir, "gateway.yaml")
	require.NoError(t, os.WriteFile(cfgPath, cfgContent, 0644))

	buf := &syncBuffer{}
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"start", "--config", cfgPath})

	// Run start in a goroutine; it blocks until signal.
	done := make(chan error, 1)
	go func() {
		done <- rootCmd.Execute()
	}()

	// Wait for the server to be listening.
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return true
		}
		return false
	}, 3*time.Second, 50*time.Millisecond)

	// Verify banner was printed.
	assert.Contains(t, buf.String(), "LightLayer Gateway")
	assert.Contains(t, buf.String(), "Ready to proxy agent traffic")
}
