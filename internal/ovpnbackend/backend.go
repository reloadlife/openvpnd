package ovpnbackend

import (
	"context"
	"time"
)

// LiveInstance is observed process state.
type LiveInstance struct {
	Name      string
	PID       int
	Up        bool
	MgmtPath  string
	ConfPath  string
	Binary    string
	RxBytes   int64
	TxBytes   int64
	Clients   []LiveClient
	UpdatedAt time.Time
}

// LiveClient is a connected client from management status.
type LiveClient struct {
	CommonName     string
	RealAddress    string
	VirtualAddress string
	ConnectedSince time.Time
	RxBytes        int64
	TxBytes        int64
}

// DesiredInstance is conf + process intent for the backend.
type DesiredInstance struct {
	Name           string
	Role           string
	Enabled        bool
	BinaryPath     string
	ConfPath       string
	ConfContent    string
	ConfHash       string
	PIDPath        string
	MgmtPath       string
	StatusPath     string
	CCDDir         string
	CCDFiles       map[string]string // filename -> content
	PreUp          string
	PostUp         string
	PreDown        string
	PostDown       string
	AllowHooks     bool
}

// MgmtClient talks to openvpn management interface.
type MgmtClient interface {
	Status(ctx context.Context) (LiveInstance, error)
	KillClient(ctx context.Context, cnOrAddr string) error
	Signal(ctx context.Context, sig string) error
	Close() error
}

// Backend applies and observes OpenVPN process state.
type Backend interface {
	// ProbeBinary runs openvpn --version.
	ProbeBinary(ctx context.Context, path string) (version string, err error)
	// ListLive returns managed live instances known to the backend.
	ListLive(ctx context.Context) ([]LiveInstance, error)
	// EnsureInstance writes conf/CCD and starts/restarts process as needed.
	EnsureInstance(ctx context.Context, d DesiredInstance) error
	// StopInstance stops a running instance.
	StopInstance(ctx context.Context, name string) error
	// RemoveInstance stops and removes runtime artifacts (not conf unless asked).
	RemoveInstance(ctx context.Context, name string) error
	// Management connects to an instance management socket.
	Management(ctx context.Context, name string) (MgmtClient, error)
	// WriteFile writes a file with mode 0600.
	WriteFile(path, content string) error
	// RunHook runs a shell hook if allowed.
	RunHook(ctx context.Context, hook string) error
	Close() error
}
