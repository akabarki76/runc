// Package configs provides various container-related configuration types
// used by libcontainer.
package configs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

	"github.com/opencontainers/cgroups"
	devices "github.com/opencontainers/cgroups/devices/config"
	"github.com/opencontainers/runtime-spec/specs-go"
)

type Rlimit struct {
	Type int    `json:"type"`
	Hard uint64 `json:"hard"`
	Soft uint64 `json:"soft"`
}

// IDMap represents UID/GID Mappings for User Namespaces.
type IDMap struct {
	ContainerID int64 `json:"container_id"`
	HostID      int64 `json:"host_id"`
	Size        int64 `json:"size"`
}

// Seccomp represents syscall restrictions
// By default, only the native architecture of the kernel is allowed to be used
// for syscalls. Additional architectures can be added by specifying them in
// Architectures.
type Seccomp struct {
	DefaultAction    Action                   `json:"default_action"`
	Architectures    []string                 `json:"architectures"`
	Flags            []specs.LinuxSeccompFlag `json:"flags"`
	Syscalls         []*Syscall               `json:"syscalls"`
	DefaultErrnoRet  *uint                    `json:"default_errno_ret"`
	ListenerPath     string                   `json:"listener_path,omitempty"`
	ListenerMetadata string                   `json:"listener_metadata,omitempty"`
}

// Action is taken upon rule match in Seccomp
type Action int

const (
	Kill Action = iota + 1
	Errno
	Trap
	Allow
	Trace
	Log
	Notify
	KillThread
	KillProcess
)

// Operator is a comparison operator to be used when matching syscall arguments in Seccomp
type Operator int

const (
	EqualTo Operator = iota + 1
	NotEqualTo
	GreaterThan
	GreaterThanOrEqualTo
	LessThan
	LessThanOrEqualTo
	MaskEqualTo
)

// Arg is a rule to match a specific syscall argument in Seccomp
type Arg struct {
	Index    uint     `json:"index"`
	Value    uint64   `json:"value"`
	ValueTwo uint64   `json:"value_two"`
	Op       Operator `json:"op"`
}

// Syscall is a rule to match a syscall in Seccomp
type Syscall struct {
	Name     string `json:"name"`
	Action   Action `json:"action"`
	ErrnoRet *uint  `json:"errnoRet"`
	Args     []*Arg `json:"args"`
}

// Config defines configuration options for executing a process inside a contained environment.
type Config struct {
	// NoPivotRoot will use MS_MOVE and a chroot to jail the process into the container's rootfs
	// This is a common option when the container is running in ramdisk.
	NoPivotRoot bool `json:"no_pivot_root,omitempty"`

	// ParentDeathSignal specifies the signal that is sent to the container's process in the case
	// that the parent process dies.
	ParentDeathSignal int `json:"parent_death_signal,omitempty"`

	// Path to a directory containing the container's root filesystem.
	Rootfs string `json:"rootfs"`

	// Umask is the umask to use inside of the container.
	Umask *uint32 `json:"umask,omitempty"`

	// Readonlyfs will remount the container's rootfs as readonly where only externally mounted
	// bind mounts are writtable.
	Readonlyfs bool `json:"readonlyfs,omitempty"`

	// Specifies the mount propagation flags to be applied to /.
	RootPropagation int `json:"rootPropagation,omitempty"`

	// Mounts specify additional source and destination paths that will be mounted inside the container's
	// rootfs and mount namespace if specified.
	Mounts []*Mount `json:"mounts"`

	// The device nodes that should be automatically created within the container upon container start.  Note, make sure that the node is marked as allowed in the cgroup as well!
	Devices []*devices.Device `json:"devices"`

	// NetDevices are key-value pairs, keyed by network device name, moved to the container's network namespace.
	NetDevices map[string]*LinuxNetDevice `json:"netDevices,omitempty"`

	MountLabel string `json:"mount_label,omitempty"`

	// Hostname optionally sets the container's hostname if provided.
	Hostname string `json:"hostname,omitempty"`

	// Domainname optionally sets the container's domainname if provided.
	Domainname string `json:"domainname,omitempty"`

	// Namespaces specifies the container's namespaces that it should setup when cloning the init process
	// If a namespace is not provided that namespace is shared from the container's parent process.
	Namespaces Namespaces `json:"namespaces"`

	// Capabilities specify the capabilities to keep when executing the process inside the container
	// All capabilities not specified will be dropped from the processes capability mask.
	Capabilities *Capabilities `json:"capabilities,omitempty"`

	// Networks specifies the container's network setup to be created.
	Networks []*Network `json:"networks,omitempty"`

	// Routes can be specified to create entries in the route table as the container is started.
	Routes []*Route `json:"routes,omitempty"`

	// Cgroups specifies specific cgroup settings for the various subsystems that the container is
	// placed into to limit the resources the container has available.
	Cgroups *cgroups.Cgroup `json:"cgroups"`

	// AppArmorProfile specifies the profile to apply to the process running in the container and is
	// change at the time the process is executed.
	AppArmorProfile string `json:"apparmor_profile,omitempty"`

	// ProcessLabel specifies the label to apply to the process running in the container.  It is
	// commonly used by selinux.
	ProcessLabel string `json:"process_label,omitempty"`

	// Rlimits specifies the resource limits, such as max open files, to set in the container
	// If Rlimits are not set, the container will inherit rlimits from the parent process.
	Rlimits []Rlimit `json:"rlimits,omitempty"`

	// OomScoreAdj specifies the adjustment to be made by the kernel when calculating oom scores
	// for a process. Valid values are between the range [-1000, '1000'], where processes with
	// higher scores are preferred for being killed. If it is unset then we don't touch the current
	// value.
	// More information about kernel oom score calculation here: https://lwn.net/Articles/317814/
	OomScoreAdj *int `json:"oom_score_adj,omitempty"`

	// UIDMappings is an array of User ID mappings for User Namespaces.
	UIDMappings []IDMap `json:"uid_mappings,omitempty"`

	// GIDMappings is an array of Group ID mappings for User Namespaces.
	GIDMappings []IDMap `json:"gid_mappings,omitempty"`

	// MaskPaths specifies paths within the container's rootfs to mask over with a bind
	// mount pointing to /dev/null as to prevent reads of the file.
	MaskPaths []string `json:"mask_paths,omitempty"`

	// ReadonlyPaths specifies paths within the container's rootfs to remount as read-only
	// so that these files prevent any writes.
	ReadonlyPaths []string `json:"readonly_paths,omitempty"`

	// Sysctl is a map of properties and their values. It is the equivalent of using
	// sysctl -w my.property.name value in Linux.
	Sysctl map[string]string `json:"sysctl,omitempty"`

	// Seccomp allows actions to be taken whenever a syscall is made within the container.
	// A number of rules are given, each having an action to be taken if a syscall matches it.
	// A default action to be taken if no rules match is also given.
	Seccomp *Seccomp `json:"seccomp,omitempty"`

	// NoNewPrivileges controls whether processes in the container can gain additional privileges.
	NoNewPrivileges bool `json:"no_new_privileges,omitempty"`

	// Hooks are a collection of actions to perform at various container lifecycle events.
	// CommandHooks are serialized to JSON, but other hooks are not.
	Hooks Hooks `json:"Hooks,omitempty"`

	// Version is the version of opencontainer specification that is supported.
	Version string `json:"version"`

	// Labels are user defined metadata that is stored in the config and populated on the state
	Labels []string `json:"labels"`

	// NoNewKeyring will not allocated a new session keyring for the container.  It will use the
	// callers keyring in this case.
	NoNewKeyring bool `json:"no_new_keyring,omitempty"`

	// IntelRdt specifies settings for Intel RDT group that the container is placed into
	// to limit the resources (e.g., L3 cache, memory bandwidth) the container has available
	IntelRdt *IntelRdt `json:"intel_rdt,omitempty"`

	// RootlessEUID is set when the runc was launched with non-zero EUID.
	// Note that RootlessEUID is set to false when launched with EUID=0 in userns.
	// When RootlessEUID is set, runc creates a new userns for the container.
	// (config.json needs to contain userns settings)
	RootlessEUID bool `json:"rootless_euid,omitempty"`

	// RootlessCgroups is set when unlikely to have the full access to cgroups.
	// When RootlessCgroups is set, cgroups errors are ignored.
	RootlessCgroups bool `json:"rootless_cgroups,omitempty"`

	// TimeOffsets specifies the offset for supporting time namespaces.
	TimeOffsets map[string]specs.LinuxTimeOffset `json:"time_offsets,omitempty"`

	// Scheduler represents the scheduling attributes for a process.
	Scheduler *Scheduler `json:"scheduler,omitempty"`

	// Personality contains configuration for the Linux personality syscall.
	Personality *LinuxPersonality `json:"personality,omitempty"`

	// IOPriority is the container's I/O priority.
	IOPriority *IOPriority `json:"io_priority,omitempty"`

	// ExecCPUAffinity is CPU affinity for a non-init process to be run in the container.
	ExecCPUAffinity *CPUAffinity `json:"exec_cpu_affinity,omitempty"`
}

// Scheduler is based on the Linux sched_setattr(2) syscall.
type Scheduler = specs.Scheduler

// ToSchedAttr is to convert *configs.Scheduler to *unix.SchedAttr.
func ToSchedAttr(scheduler *Scheduler) (*unix.SchedAttr, error) {
	var policy uint32
	switch scheduler.Policy {
	case specs.SchedOther:
		policy = 0
	case specs.SchedFIFO:
		policy = 1
	case specs.SchedRR:
		policy = 2
	case specs.SchedBatch:
		policy = 3
	case specs.SchedISO:
		policy = 4
	case specs.SchedIdle:
		policy = 5
	case specs.SchedDeadline:
		policy = 6
	default:
		return nil, fmt.Errorf("invalid scheduler policy: %s", scheduler.Policy)
	}

	var flags uint64
	for _, flag := range scheduler.Flags {
		switch flag {
		case specs.SchedFlagResetOnFork:
			flags |= 0x01
		case specs.SchedFlagReclaim:
			flags |= 0x02
		case specs.SchedFlagDLOverrun:
			flags |= 0x04
		case specs.SchedFlagKeepPolicy:
			flags |= 0x08
		case specs.SchedFlagKeepParams:
			flags |= 0x10
		case specs.SchedFlagUtilClampMin:
			flags |= 0x20
		case specs.SchedFlagUtilClampMax:
			flags |= 0x40
		default:
			return nil, fmt.Errorf("invalid scheduler flag: %s", flag)
		}
	}

	return &unix.SchedAttr{
		Size:     unix.SizeofSchedAttr,
		Policy:   policy,
		Flags:    flags,
		Nice:     scheduler.Nice,
		Priority: uint32(scheduler.Priority),
		Runtime:  scheduler.Runtime,
		Deadline: scheduler.Deadline,
		Period:   scheduler.Period,
	}, nil
}

type IOPriority = specs.LinuxIOPriority

type CPUAffinity struct {
	Initial, Final *unix.CPUSet
}

func toCPUSet(str string) (*unix.CPUSet, error) {
	if str == "" {
		return nil, nil
	}
	s := new(unix.CPUSet)

	// Since (*CPUset).Set silently ignores too high CPU values,
	// find out what the maximum is, and return an error.
	maxCPU := uint64(unsafe.Sizeof(*s) * 8)
	toInt := func(v string) (int, error) {
		ret, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return 0, err
		}
		if ret >= maxCPU {
			return 0, fmt.Errorf("values larger than %d are not supported", maxCPU-1)
		}
		return int(ret), nil
	}

	for _, r := range strings.Split(str, ",") {
		// Allow extra spaces around.
		r = strings.TrimSpace(r)
		// Allow empty elements (extra commas).
		if r == "" {
			continue
		}
		if r0, r1, found := strings.Cut(r, "-"); found {
			start, err := toInt(r0)
			if err != nil {
				return nil, err
			}
			end, err := toInt(r1)
			if err != nil {
				return nil, err
			}
			if start > end {
				return nil, errors.New("invalid range: " + r)
			}
			for i := start; i <= end; i++ {
				s.Set(i)
			}
		} else {
			val, err := toInt(r)
			if err != nil {
				return nil, err
			}
			s.Set(val)
		}
	}
	if s.Count() == 0 {
		return nil, fmt.Errorf("no CPUs found in %q", str)
	}

	return s, nil
}

// ConvertCPUAffinity converts [specs.CPUAffinity] to [CPUAffinity].
func ConvertCPUAffinity(sa *specs.CPUAffinity) (*CPUAffinity, error) {
	if sa == nil {
		return nil, nil
	}
	initial, err := toCPUSet(sa.Initial)
	if err != nil {
		return nil, fmt.Errorf("bad CPUAffinity.Initial: %w", err)
	}
	final, err := toCPUSet(sa.Final)
	if err != nil {
		return nil, fmt.Errorf("bad CPUAffinity.Final: %w", err)
	}
	if initial == nil && final == nil {
		return nil, nil
	}

	return &CPUAffinity{
		Initial: initial,
		Final:   final,
	}, nil
}

type (
	HookName string
	HookList []Hook
	Hooks    map[HookName]HookList
)

const (
	// Prestart commands are executed after the container namespaces are created,
	// but before the user supplied command is executed from init.
	// Note: This hook is now deprecated
	// Prestart commands are called in the Runtime namespace.
	Prestart HookName = "prestart"

	// CreateRuntime commands MUST be called as part of the create operation after
	// the runtime environment has been created but before the pivot_root has been executed.
	// CreateRuntime is called immediately after the deprecated Prestart hook.
	// CreateRuntime commands are called in the Runtime Namespace.
	CreateRuntime HookName = "createRuntime"

	// CreateContainer commands MUST be called as part of the create operation after
	// the runtime environment has been created but before the pivot_root has been executed.
	// CreateContainer commands are called in the Container namespace.
	CreateContainer HookName = "createContainer"

	// StartContainer commands MUST be called as part of the start operation and before
	// the container process is started.
	// StartContainer commands are called in the Container namespace.
	StartContainer HookName = "startContainer"

	// Poststart commands are executed after the container init process starts.
	// Poststart commands are called in the Runtime Namespace.
	Poststart HookName = "poststart"

	// Poststop commands are executed after the container init process exits.
	// Poststop commands are called in the Runtime Namespace.
	Poststop HookName = "poststop"
)

// HasHook checks if config has any hooks with any given names configured.
func (c *Config) HasHook(names ...HookName) bool {
	if c.Hooks == nil {
		return false
	}
	for _, h := range names {
		if len(c.Hooks[h]) > 0 {
			return true
		}
	}
	return false
}

// KnownHookNames returns the known hook names.
// Used by `runc features`.
func KnownHookNames() []string {
	return []string{
		string(Prestart), // deprecated
		string(CreateRuntime),
		string(CreateContainer),
		string(StartContainer),
		string(Poststart),
		string(Poststop),
	}
}

type Capabilities struct {
	// Bounding is the set of capabilities checked by the kernel.
	Bounding []string `json:"Bounding,omitempty"`
	// Effective is the set of capabilities checked by the kernel.
	Effective []string `json:"Effective,omitempty"`
	// Inheritable is the capabilities preserved across execve.
	Inheritable []string `json:"Inheritable,omitempty"`
	// Permitted is the limiting superset for effective capabilities.
	Permitted []string `json:"Permitted,omitempty"`
	// Ambient is the ambient set of capabilities that are kept.
	Ambient []string `json:"Ambient,omitempty"`
}

// Deprecated: use [Hooks.Run] instead.
func (hooks HookList) RunHooks(state *specs.State) error {
	for i, h := range hooks {
		if err := h.Run(state); err != nil {
			return fmt.Errorf("error running hook #%d: %w", i, err)
		}
	}

	return nil
}

func (hooks *Hooks) UnmarshalJSON(b []byte) error {
	var state map[HookName][]CommandHook

	if err := json.Unmarshal(b, &state); err != nil {
		return err
	}

	*hooks = Hooks{}
	for n, commandHooks := range state {
		if len(commandHooks) == 0 {
			continue
		}

		(*hooks)[n] = HookList{}
		for _, h := range commandHooks {
			(*hooks)[n] = append((*hooks)[n], h)
		}
	}

	return nil
}

func (hooks *Hooks) MarshalJSON() ([]byte, error) {
	serialize := func(hooks []Hook) (serializableHooks []CommandHook) {
		for _, hook := range hooks {
			switch chook := hook.(type) {
			case CommandHook:
				serializableHooks = append(serializableHooks, chook)
			default:
				logrus.Warnf("cannot serialize hook of type %T, skipping", hook)
			}
		}

		return serializableHooks
	}

	return json.Marshal(map[string]any{
		"prestart":        serialize((*hooks)[Prestart]),
		"createRuntime":   serialize((*hooks)[CreateRuntime]),
		"createContainer": serialize((*hooks)[CreateContainer]),
		"startContainer":  serialize((*hooks)[StartContainer]),
		"poststart":       serialize((*hooks)[Poststart]),
		"poststop":        serialize((*hooks)[Poststop]),
	})
}

// Run executes all hooks for the given hook name.
func (hooks Hooks) Run(name HookName, state *specs.State) error {
	list := hooks[name]
	for i, h := range list {
		if err := h.Run(state); err != nil {
			return fmt.Errorf("error running %s hook #%d: %w", name, i, err)
		}
	}

	return nil
}

// SetDefaultEnv sets the environment for those CommandHook entries
// that do not have one set.
func (hooks HookList) SetDefaultEnv(env []string) {
	for _, h := range hooks {
		if ch, ok := h.(CommandHook); ok && len(ch.Env) == 0 {
			ch.Env = env
		}
	}
}

type Hook interface {
	// Run executes the hook with the provided state.
	Run(*specs.State) error
}

// NewFunctionHook will call the provided function when the hook is run.
func NewFunctionHook(f func(*specs.State) error) FuncHook {
	return FuncHook{
		run: f,
	}
}

type FuncHook struct {
	run func(*specs.State) error
}

func (f FuncHook) Run(s *specs.State) error {
	return f.run(s)
}

type Command struct {
	Path    string         `json:"path"`
	Args    []string       `json:"args"`
	Env     []string       `json:"env"`
	Dir     string         `json:"dir"`
	Timeout *time.Duration `json:"timeout"`
}

// NewCommandHook will execute the provided command when the hook is run.
func NewCommandHook(cmd *Command) CommandHook {
	return CommandHook{
		Command: cmd,
	}
}

type CommandHook struct {
	*Command
}

func (c *Command) Run(s *specs.State) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Cmd{
		Path:   c.Path,
		Args:   c.Args,
		Env:    c.Env,
		Stdin:  bytes.NewReader(b),
		Stdout: &stdout,
		Stderr: &stderr,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	errC := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		if err != nil {
			err = fmt.Errorf("%w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
		}
		errC <- err
	}()
	var timerCh <-chan time.Time
	if c.Timeout != nil {
		timer := time.NewTimer(*c.Timeout)
		defer timer.Stop()
		timerCh = timer.C
	}
	select {
	case err := <-errC:
		return err
	case <-timerCh:
		_ = cmd.Process.Kill()
		<-errC
		return fmt.Errorf("hook ran past specified timeout of %.1fs", c.Timeout.Seconds())
	}
}
