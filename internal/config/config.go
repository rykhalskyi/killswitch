package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	KindKustomization = "Kustomization"
	KindHelmRelease   = "HelmRelease"
	KindDeployment    = "Deployment"
	KindStatefulSet   = "StatefulSet"
	KindCronJob       = "CronJob"
	KindNode          = "Node"

	ActionFluxSuspend   = "flux.suspend"
	ActionFluxResume    = "flux.resume"
	ActionKubeScale     = "kubernetes.scale"
	ActionKubeSuspend   = "kubernetes.suspend"
	ActionTalosShutdown = "talos.shutdown"

	RequiresAny       = "any"
	RequiresSuspended = "suspended"
	RequiresActive    = "active"

	DirectionOn  = "on"
	DirectionOff = "off"
)

// Resource represents a single target resource.
type Resource struct {
	Kind      string `yaml:"kind" json:"kind"`
	Namespace string `yaml:"namespace" json:"namespace"`
	Name      string `yaml:"name" json:"name"`
}

// Key returns a unique identifier for the resource.
func (r Resource) Key() string {
	return fmt.Sprintf("%s/%s/%s", r.Kind, r.Namespace, r.Name)
}

// FluxConfig holds the legacy Flux resource list used when synthesizing the
// built-in flux switch for programmatic configurations and older state dirs.
type FluxConfig struct {
	Resources []Resource `yaml:"resources" json:"resources"`
}

// Switch is the user-visible control.
type Switch struct {
	ID          string `yaml:"id" json:"id"`
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	Icon        string `yaml:"icon,omitempty" json:"icon,omitempty"`
	Workflow    string `yaml:"workflow" json:"workflow"`
}

// Requires declares workflow prerequisites.
type Requires struct {
	Flux string `yaml:"flux" json:"flux"`
}

// StateValues is a desired-state fragment (replicas, suspend, restore, default).
type StateValues map[string]any

// Step is one target and its desired values per switch position.
type Step struct {
	ID     string     `yaml:"id" json:"id"`
	Action string     `yaml:"action" json:"action"`
	Target Resource   `yaml:"target" json:"target"`
	States StepStates `yaml:"states" json:"states"`
}

type StepStates struct {
	On  StateValues `yaml:"on" json:"on"`
	Off StateValues `yaml:"off" json:"off"`
}

// Workflow is an ordered list of steps.
type Workflow struct {
	ID       string   `yaml:"id" json:"id"`
	Name     string   `yaml:"name" json:"name"`
	Requires Requires `yaml:"requires" json:"requires"`
	Steps    []Step   `yaml:"steps" json:"steps"`
	OnError  string   `yaml:"on_error" json:"on_error"`
	// OneWay marks a workflow as a one-off action with no ON side. It runs in
	// declaration order for OFF and refuses ON transitions.
	OneWay bool `yaml:"one_way,omitempty" json:"one_way,omitempty"`
}

// SwitchesFile is the root switches.yaml structure.
type SwitchesFile struct {
	Switches []Switch `yaml:"switches" json:"switches"`
}

// Config is the normalized in-memory configuration.
type Config struct {
	Switches  []Switch             `yaml:"switches" json:"switches"`
	Workflows map[string]*Workflow `yaml:"-" json:"workflows"`
	Flux      FluxConfig           `yaml:"flux" json:"flux"`
}

// Load reads a config directory or a switches.yaml file.
func Load(path string) (*Config, error) {
	base, switchesPath, err := configBase(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{Workflows: make(map[string]*Workflow)}

	data, err := os.ReadFile(switchesPath)
	if err != nil {
		return nil, fmt.Errorf("reading switches config %s: %w", switchesPath, err)
	}
	var sf SwitchesFile
	if err := yaml.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("parsing switches YAML: %w", err)
	}
	cfg.Switches = sf.Switches

	workflowDir := filepath.Join(base, "workflows")
	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading workflow directory %s: %w", workflowDir, err)
		}
	} else {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if ext != ".yaml" && ext != ".yml" {
				continue
			}
			wfPath := filepath.Join(workflowDir, entry.Name())
			wfData, readErr := os.ReadFile(wfPath)
			if readErr != nil {
				return nil, fmt.Errorf("reading workflow %s: %w", wfPath, readErr)
			}
			var wf Workflow
			if unmarshalErr := yaml.Unmarshal(wfData, &wf); unmarshalErr != nil {
				return nil, fmt.Errorf("parsing workflow %s: %w", wfPath, unmarshalErr)
			}
			if wf.ID == "" {
				wf.ID = strings.TrimSuffix(entry.Name(), ext)
			}
			if _, exists := cfg.Workflows[wf.ID]; exists {
				return nil, fmt.Errorf("duplicate workflow id %q", wf.ID)
			}
			cfg.Workflows[wf.ID] = &wf
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	cfg.Normalize()
	return cfg, nil
}

// configBase resolves a --config argument to the config directory and switches file.
func configBase(path string) (string, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("config path %s: %w", path, err)
	}
	if info.IsDir() {
		switchesPath := filepath.Join(path, "switches.yaml")
		if _, statErr := os.Stat(switchesPath); statErr != nil {
			switchesPath = filepath.Join(path, "switches.yml")
			if _, statErr := os.Stat(switchesPath); statErr != nil {
				return "", "", fmt.Errorf("no switches.yaml or switches.yml in %s", path)
			}
		}
		return path, switchesPath, nil
	}
	base := filepath.Dir(path)
	// Allow an arbitrary file name containing a switches document.
	data, err := os.ReadFile(path)
	if err == nil {
		var probe struct {
			Switches []Switch `yaml:"switches"`
		}
		if yaml.Unmarshal(data, &probe) == nil && len(probe.Switches) > 0 {
			return base, path, nil
		}
	}
	return "", "", errors.New("legacy config format detected: Phase 3 requires switches.yaml plus workflows/*.yaml")
}

// Normalize synthesizes the built-in flux workflow for programmatic configs.
func (c *Config) Normalize() {
	if c.Workflows == nil {
		c.Workflows = make(map[string]*Workflow)
	}
	if len(c.Workflows) > 0 || len(c.Flux.Resources) == 0 {
		return
	}
	wf := fluxWorkflow(c.Flux.Resources)
	c.Workflows[wf.ID] = wf
	if c.SwitchByID("flux") == nil {
		c.Switches = append([]Switch{{
			ID:          "flux",
			Name:        "Flux Reconciliation",
			Description: "Suspend / restore all configured Flux Kustomizations and HelmReleases",
			Workflow:    wf.ID,
		}}, c.Switches...)
	}
}

// fluxWorkflow builds the canonical flux workflow in ON (restore) order.
func fluxWorkflow(resources []Resource) *Workflow {
	var helm, kustomizations []Resource
	for _, res := range resources {
		switch res.Kind {
		case KindHelmRelease:
			helm = append(helm, res)
		case KindKustomization:
			kustomizations = append(kustomizations, res)
		}
	}
	wf := &Workflow{ID: "flux", Name: "Flux reconciliation", Requires: Requires{Flux: RequiresAny}}
	order := append(append([]Resource{}, helm...), kustomizations...)
	for i, res := range order {
		wf.Steps = append(wf.Steps, Step{
			ID:     fmt.Sprintf("%02d-%s-%s", i+1, strings.ToLower(res.Kind), res.Name),
			Action: ActionFluxSuspend,
			Target: res,
			States: StepStates{
				Off: StateValues{"suspend": true},
				On:  StateValues{"restore": "previous", "default": false},
			},
		})
	}
	return wf
}

// SwitchByID returns the switch with the given id.
func (c *Config) SwitchByID(id string) *Switch {
	for i := range c.Switches {
		if c.Switches[i].ID == id {
			return &c.Switches[i]
		}
	}
	return nil
}

// FluxSwitchID returns the id of the Flux switch.
func (c *Config) FluxSwitchID() string {
	if c.SwitchByID("flux") != nil {
		return "flux"
	}
	for _, sw := range c.Switches {
		wf := c.Workflows[sw.Workflow]
		if wf != nil && len(wf.Steps) > 0 && strings.HasPrefix(wf.Steps[0].Action, "flux.") {
			return sw.ID
		}
	}
	return ""
}

// WorkflowForSwitch returns the workflow bound to a switch.
func (c *Config) WorkflowForSwitch(id string) *Workflow {
	sw := c.SwitchByID(id)
	if sw == nil {
		return nil
	}
	return c.Workflows[sw.Workflow]
}

// AllWorkflows returns workflows sorted by id.
func (c *Config) AllWorkflows() []*Workflow {
	ids := make([]string, 0, len(c.Workflows))
	for id := range c.Workflows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*Workflow, 0, len(ids))
	for _, id := range ids {
		out = append(out, c.Workflows[id])
	}
	return out
}

// Validate checks the layered configuration invariants.
func (c *Config) Validate() error {
	// Legacy programmatic config: validate the flux resource list.
	if len(c.Switches) == 0 && len(c.Workflows) == 0 {
		return c.validateLegacy()
	}
	if len(c.Switches) == 0 {
		return errors.New("switches must contain at least one switch")
	}
	if len(c.Workflows) == 0 {
		return errors.New("at least one workflow must be defined")
	}

	switchIDs := make(map[string]bool)
	for i, sw := range c.Switches {
		if sw.ID == "" {
			return fmt.Errorf("switch #%d id cannot be empty", i+1)
		}
		if sw.Name == "" {
			return fmt.Errorf("switch %q name cannot be empty", sw.ID)
		}
		if strings.ContainsAny(sw.Icon, "\n\r\t") {
			return fmt.Errorf("switch %q icon must be a single-line symbol", sw.ID)
		}
		if sw.Workflow == "" {
			return fmt.Errorf("switch %q workflow cannot be empty", sw.ID)
		}
		if switchIDs[sw.ID] {
			return fmt.Errorf("duplicate switch id %q", sw.ID)
		}
		switchIDs[sw.ID] = true
		if _, ok := c.Workflows[sw.Workflow]; !ok {
			return fmt.Errorf("switch %q references unknown workflow %q", sw.ID, sw.Workflow)
		}
	}

	for id, wf := range c.Workflows {
		if wf.ID != "" && wf.ID != id {
			return fmt.Errorf("workflow id %q does not match file id %q", wf.ID, id)
		}
		switch wf.Requires.Flux {
		case "", RequiresAny, RequiresSuspended, RequiresActive:
		default:
			return fmt.Errorf("workflow %q has invalid requires.flux %q", id, wf.Requires.Flux)
		}
		if wf.OneWay && wf.Requires.Flux != "" && wf.Requires.Flux != RequiresAny {
			return fmt.Errorf("workflow %q is one-way and cannot require flux=%s", id, wf.Requires.Flux)
		}
		stepIDs := make(map[string]bool)
		for i, step := range wf.Steps {
			if step.ID == "" {
				return fmt.Errorf("workflow %q step #%d id cannot be empty", id, i+1)
			}
			if stepIDs[step.ID] {
				return fmt.Errorf("workflow %q has duplicate step id %q", id, step.ID)
			}
			stepIDs[step.ID] = true
			if err := c.validateStep(id, step); err != nil {
				return err
			}
		}
	}

	return c.validateNoCycles()
}

func (c *Config) validateStep(workflowID string, step Step) error {
	switch step.Action {
	case ActionFluxSuspend, ActionFluxResume:
		if step.Target.Kind != KindKustomization && step.Target.Kind != KindHelmRelease {
			return fmt.Errorf("workflow %q step %q: flux actions require Kustomization or HelmRelease, got %q", workflowID, step.ID, step.Target.Kind)
		}
	case ActionKubeScale:
		if step.Target.Kind != KindDeployment && step.Target.Kind != KindStatefulSet {
			return fmt.Errorf("workflow %q step %q: kubernetes.scale supports Deployment or StatefulSet, got %q", workflowID, step.ID, step.Target.Kind)
		}
	case ActionKubeSuspend:
		if step.Target.Kind != KindCronJob {
			return fmt.Errorf("workflow %q step %q: kubernetes.suspend supports CronJob, got %q", workflowID, step.ID, step.Target.Kind)
		}
	case ActionTalosShutdown:
		if step.Target.Kind != KindNode {
			return fmt.Errorf("workflow %q step %q: talos.shutdown supports Node, got %q", workflowID, step.ID, step.Target.Kind)
		}
	default:
		return fmt.Errorf("workflow %q step %q: unsupported action %q", workflowID, step.ID, step.Action)
	}
	if step.Target.Name == "" {
		return fmt.Errorf("workflow %q step %q target name cannot be empty", workflowID, step.ID)
	}
	// Node targets are cluster-scoped, so they carry no namespace.
	if step.Target.Kind != KindNode && step.Target.Namespace == "" {
		return fmt.Errorf("workflow %q step %q target namespace cannot be empty", workflowID, step.ID)
	}
	return nil
}

// validateNoCycles rejects self-referential switches. Only the Flux interlock
// exists in Phase 3, so a separate dependency graph is not built.
func (c *Config) validateNoCycles() error {
	fluxID := c.FluxSwitchID()
	for _, sw := range c.Switches {
		wf := c.Workflows[sw.Workflow]
		if wf == nil {
			continue
		}
		if wf.Requires.Flux != "" && wf.Requires.Flux != RequiresAny && sw.ID == fluxID {
			return fmt.Errorf("flux switch %q cannot require flux=%s", sw.ID, wf.Requires.Flux)
		}
	}
	return nil
}

// validateLegacy preserves programmatic flux-only configs used by older tests.
func (c *Config) validateLegacy() error {
	if len(c.Flux.Resources) == 0 {
		return errors.New("flux.resources must contain at least one resource")
	}
	seen := make(map[string]bool)
	for i, res := range c.Flux.Resources {
		if res.Kind != KindKustomization && res.Kind != KindHelmRelease {
			return fmt.Errorf("resource #%d has unsupported kind %q (must be %s or %s)", i+1, res.Kind, KindKustomization, KindHelmRelease)
		}
		if res.Namespace == "" {
			return fmt.Errorf("resource #%d (%s) namespace cannot be empty", i+1, res.Kind)
		}
		if res.Name == "" {
			return fmt.Errorf("resource #%d (%s/%s) name cannot be empty", i+1, res.Kind, res.Namespace)
		}
		key := res.Key()
		if seen[key] {
			return fmt.Errorf("duplicate resource %q found at index #%d", key, i+1)
		}
		seen[key] = true
	}
	return nil
}

// BoolValue reads a boolean fragment.
func (s StateValues) BoolValue(key string) (bool, bool) {
	v, ok := s[key]
	if !ok {
		return false, false
	}
	switch t := v.(type) {
	case bool:
		return t, true
	default:
		return false, false
	}
}

// IntValue reads an integer fragment.
func (s StateValues) IntValue(key string) (int, bool) {
	v, ok := s[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case int:
		return t, true
	case int32:
		return int(t), true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	default:
		return 0, false
	}
}

// StringValue reads a string fragment.
func (s StateValues) StringValue(key string) (string, bool) {
	v, ok := s[key]
	if !ok {
		return "", false
	}
	t, ok := v.(string)
	return t, ok
}
