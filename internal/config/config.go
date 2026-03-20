package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type ServerConfig struct {
	Port    int    `json:"port"`
	Host    string `json:"host"`
	Timeout int    `json:"timeout"`
}

type UpstreamService struct {
	Name                    string   `json:"name"`
	BaseURL                 string   `json:"base_url"`
	APIKey                  string   `json:"api_key"`
	Models                  []string `json:"models"`
	ClientKeys              []string `json:"client_keys"`
	Description             string   `json:"description"`
	PromptInjectionRole     string   `json:"prompt_injection_role"`
	PromptInjectionTarget   string   `json:"prompt_injection_target"`
	SoftToolProtocol        string   `json:"soft_tool_calling_protocol"`
	SoftToolPromptProfileID string   `json:"soft_tool_prompt_profile_id"`
	UpstreamProtocol        string   `json:"upstream_protocol"`
	IsDefault               bool     `json:"is_default"`
}

type SoftToolPromptProfile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Protocol    string `json:"protocol"`
	Template    string `json:"template"`
	Enabled     bool   `json:"enabled"`
}

type ModelRoute struct {
	Service        UpstreamService
	ServiceName    string
	RequestedModel string
	ActualModel    string
	ModelEntry     string
}

type FeaturesConfig struct {
	EnableFunctionCalling          bool   `json:"enable_function_calling"`
	LogLevel                       string `json:"log_level"`
	ConvertDeveloperToSystem       bool   `json:"convert_developer_to_system"`
	PromptTemplate                 string `json:"prompt_template"`
	DefaultSoftToolPromptProfileID string `json:"default_soft_tool_prompt_profile_id"`
	PromptInjectionRole            string `json:"prompt_injection_role"`
	PromptInjectionTarget          string `json:"prompt_injection_target"`
	SoftToolProtocol               string `json:"soft_tool_calling_protocol"`
	KeyPassthrough                 bool   `json:"key_passthrough"`
	ModelPassthrough               bool   `json:"model_passthrough"`
}

type AppConfig struct {
	Server                 ServerConfig            `json:"server"`
	UpstreamServices       []UpstreamService       `json:"upstream_services"`
	SoftToolPromptProfiles []SoftToolPromptProfile `json:"soft_tool_prompt_profiles"`
	Features               FeaturesConfig          `json:"features"`
}

type RoutingTable struct {
	RequestedModelToRoutes map[string][]ModelRoute
	AliasToModels          map[string][]string
	KeyToServices          map[string][]UpstreamService
	DefaultService         UpstreamService
}

func (c *AppConfig) applyDefaults() {
	if c.Server.Port == 0 {
		c.Server.Port = 8000
	}
	if strings.TrimSpace(c.Server.Host) == "" {
		c.Server.Host = "0.0.0.0"
	}
	if c.Server.Timeout == 0 {
		c.Server.Timeout = 180
	}
	if strings.TrimSpace(c.Features.LogLevel) == "" {
		c.Features.LogLevel = "INFO"
	}
	if strings.TrimSpace(c.Features.SoftToolProtocol) == "" {
		c.Features.SoftToolProtocol = SoftToolProtocolXML
	}
}

const (
	UpstreamProtocolOpenAICompat = "openai_compat"
	UpstreamProtocolResponses    = "responses"
	UpstreamProtocolAnthropic    = "anthropic"

	PromptInjectionTargetAuto         = "auto"
	PromptInjectionTargetMessage      = "message"
	PromptInjectionTargetSystem       = "system"
	PromptInjectionTargetInstructions = "instructions"

	SoftToolProtocolXML           = "xml"
	SoftToolProtocolSentinelJSON  = "sentinel_json"
	SoftToolProtocolMarkdownBlock = "markdown_block"
)

var (
	promptToolCatalogPlaceholders = []string{"{tool_catalog}"}
	promptProtocolPlaceholders    = []string{"{trigger_signal}", "{protocol_rules}", "{single_call_example}", "{multi_call_example}"}
)

func NormalizeSoftToolProtocol(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", SoftToolProtocolXML:
		return SoftToolProtocolXML, true
	case SoftToolProtocolSentinelJSON:
		return SoftToolProtocolSentinelJSON, true
	case SoftToolProtocolMarkdownBlock:
		return SoftToolProtocolMarkdownBlock, true
	default:
		return "", false
	}
}

func NormalizeUpstreamProtocol(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", UpstreamProtocolOpenAICompat:
		return UpstreamProtocolOpenAICompat, true
	case UpstreamProtocolResponses:
		return UpstreamProtocolResponses, true
	case UpstreamProtocolAnthropic:
		return UpstreamProtocolAnthropic, true
	default:
		return "", false
	}
}

func NormalizePromptInjectionTarget(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", PromptInjectionTargetAuto:
		return PromptInjectionTargetAuto, true
	case PromptInjectionTargetMessage:
		return PromptInjectionTargetMessage, true
	case PromptInjectionTargetSystem:
		return PromptInjectionTargetSystem, true
	case PromptInjectionTargetInstructions:
		return PromptInjectionTargetInstructions, true
	default:
		return "", false
	}
}

func (c *AppConfig) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return errors.New("server.port must be between 1 and 65535")
	}
	if c.Server.Timeout < 1 {
		return errors.New("server.timeout must be greater than 0")
	}

	validLevels := map[string]struct{}{
		"DEBUG": {}, "INFO": {}, "WARNING": {}, "ERROR": {}, "CRITICAL": {}, "DISABLED": {},
	}
	c.Features.LogLevel = strings.ToUpper(strings.TrimSpace(c.Features.LogLevel))
	if _, ok := validLevels[c.Features.LogLevel]; !ok {
		return fmt.Errorf("features.log_level must be one of DEBUG, INFO, WARNING, ERROR, CRITICAL, DISABLED")
	}
	c.Features.PromptTemplate = strings.TrimSpace(c.Features.PromptTemplate)
	if c.Features.PromptTemplate != "" {
		if err := validatePromptTemplate(c.Features.PromptTemplate, "features.prompt_template"); err != nil {
			return err
		}
	}
	c.Features.DefaultSoftToolPromptProfileID = strings.TrimSpace(c.Features.DefaultSoftToolPromptProfileID)
	c.Features.PromptInjectionRole = strings.TrimSpace(c.Features.PromptInjectionRole)
	injectionTarget, ok := NormalizePromptInjectionTarget(c.Features.PromptInjectionTarget)
	if !ok {
		return fmt.Errorf("features.prompt_injection_target must be one of %s, %s, %s, %s", PromptInjectionTargetAuto, PromptInjectionTargetMessage, PromptInjectionTargetSystem, PromptInjectionTargetInstructions)
	}
	c.Features.PromptInjectionTarget = injectionTarget
	protocol, ok := NormalizeSoftToolProtocol(c.Features.SoftToolProtocol)
	if !ok {
		return fmt.Errorf(
			"features.soft_tool_calling_protocol must be one of %s, %s, %s",
			SoftToolProtocolXML,
			SoftToolProtocolSentinelJSON,
			SoftToolProtocolMarkdownBlock,
		)
	}
	c.Features.SoftToolProtocol = protocol

	profileIDs := map[string]struct{}{}
	profileNames := map[string]string{}
	enabledProfileIDs := map[string]struct{}{}
	for i := range c.SoftToolPromptProfiles {
		profile := &c.SoftToolPromptProfiles[i]
		profile.ID = strings.TrimSpace(profile.ID)
		profile.Name = strings.TrimSpace(profile.Name)
		profile.Description = strings.TrimSpace(profile.Description)
		profile.Template = strings.TrimSpace(profile.Template)
		profile.Protocol = strings.TrimSpace(profile.Protocol)

		if profile.ID == "" {
			return fmt.Errorf("soft_tool_prompt_profiles[%d].id cannot be empty", i)
		}
		if profile.Name == "" {
			return fmt.Errorf("soft_tool_prompt_profiles[%d].name cannot be empty", i)
		}
		if _, exists := profileIDs[profile.ID]; exists {
			return fmt.Errorf("duplicate soft tool prompt profile id %q", profile.ID)
		}
		profileIDs[profile.ID] = struct{}{}

		nameKey := strings.ToLower(profile.Name)
		if existingID, exists := profileNames[nameKey]; exists {
			return fmt.Errorf("duplicate soft tool prompt profile name %q for ids %q and %q", profile.Name, existingID, profile.ID)
		}
		profileNames[nameKey] = profile.ID

		if profile.Protocol != "" {
			normalizedProtocol, ok := NormalizeSoftToolProtocol(profile.Protocol)
			if !ok {
				return fmt.Errorf(
					"soft_tool_prompt_profiles[%d].protocol must be one of %s, %s, %s",
					i,
					SoftToolProtocolXML,
					SoftToolProtocolSentinelJSON,
					SoftToolProtocolMarkdownBlock,
				)
			}
			profile.Protocol = normalizedProtocol
		}
		if err := validatePromptTemplate(profile.Template, fmt.Sprintf("soft_tool_prompt_profiles[%d].template", i)); err != nil {
			return err
		}
		if profile.Enabled {
			enabledProfileIDs[profile.ID] = struct{}{}
		}
	}

	if c.Features.DefaultSoftToolPromptProfileID != "" {
		if _, ok := enabledProfileIDs[c.Features.DefaultSoftToolPromptProfileID]; !ok {
			return fmt.Errorf("features.default_soft_tool_prompt_profile_id %q must reference an enabled soft tool prompt profile", c.Features.DefaultSoftToolPromptProfileID)
		}
	}

	if len(c.UpstreamServices) == 0 {
		return nil
	}

	defaultCount := 0
	keyModelOwners := map[string]map[string]string{}

	for i := range c.UpstreamServices {
		svc := &c.UpstreamServices[i]
		svc.BaseURL = strings.TrimRight(strings.TrimSpace(svc.BaseURL), "/")
		svc.APIKey = strings.TrimSpace(svc.APIKey)
		svc.Name = strings.TrimSpace(svc.Name)
		svc.ClientKeys = normalizeStrings(svc.ClientKeys)
		svc.PromptInjectionRole = strings.TrimSpace(svc.PromptInjectionRole)
		svc.PromptInjectionTarget = strings.ToLower(strings.TrimSpace(svc.PromptInjectionTarget))
		if svc.PromptInjectionTarget != "" {
			promptTarget, ok := NormalizePromptInjectionTarget(svc.PromptInjectionTarget)
			if !ok {
				return fmt.Errorf("upstream service %q prompt_injection_target must be one of %s, %s, %s, %s", svc.Name, PromptInjectionTargetAuto, PromptInjectionTargetMessage, PromptInjectionTargetSystem, PromptInjectionTargetInstructions)
			}
			svc.PromptInjectionTarget = promptTarget
		}
		svc.SoftToolProtocol = strings.ToLower(strings.TrimSpace(svc.SoftToolProtocol))
		svc.SoftToolPromptProfileID = strings.TrimSpace(svc.SoftToolPromptProfileID)
		upstreamProtocol, ok := NormalizeUpstreamProtocol(svc.UpstreamProtocol)
		if !ok {
			return fmt.Errorf("upstream service %q upstream_protocol must be one of %s, %s, %s", svc.Name, UpstreamProtocolOpenAICompat, UpstreamProtocolResponses, UpstreamProtocolAnthropic)
		}
		svc.UpstreamProtocol = upstreamProtocol
		if svc.SoftToolProtocol != "" {
			if _, ok := NormalizeSoftToolProtocol(svc.SoftToolProtocol); !ok {
				return fmt.Errorf(
					"upstream service %q soft_tool_calling_protocol must be one of %s, %s, %s",
					svc.Name,
					SoftToolProtocolXML,
					SoftToolProtocolSentinelJSON,
					SoftToolProtocolMarkdownBlock,
				)
			}
		}
		if svc.SoftToolPromptProfileID != "" {
			if _, ok := enabledProfileIDs[svc.SoftToolPromptProfileID]; !ok {
				return fmt.Errorf("upstream service %q soft_tool_prompt_profile_id %q must reference an enabled soft tool prompt profile", svc.Name, svc.SoftToolPromptProfileID)
			}
		}
		if svc.Name == "" {
			return errors.New("upstream_services.name cannot be empty")
		}
		if !strings.HasPrefix(svc.BaseURL, "http://") && !strings.HasPrefix(svc.BaseURL, "https://") {
			return fmt.Errorf("upstream service %q base_url must start with http:// or https://", svc.Name)
		}
		if len(svc.Models) == 0 {
			return fmt.Errorf("upstream service %q models cannot be empty", svc.Name)
		}
		if svc.IsDefault {
			defaultCount++
		}
		serviceModels := map[string]struct{}{}
		serviceRequestedModels := map[string]struct{}{}
		for idx, rawModel := range svc.Models {
			model := strings.TrimSpace(rawModel)
			if model == "" {
				return fmt.Errorf("upstream service %q contains empty model entry", svc.Name)
			}
			if _, ok := serviceModels[model]; ok {
				return fmt.Errorf("duplicate model entry %q found in upstream service %q", model, svc.Name)
			}
			serviceModels[model] = struct{}{}
			svc.Models[idx] = model

			requestedModel := model
			if strings.Contains(model, ":") {
				parts := strings.SplitN(model, ":", 2)
				requestedModel = strings.TrimSpace(parts[0])
				actualModel := strings.TrimSpace(parts[1])
				if requestedModel == "" || actualModel == "" {
					return fmt.Errorf("invalid alias format: %s", model)
				}
			}
			if _, ok := serviceRequestedModels[requestedModel]; ok {
				return fmt.Errorf("upstream service %q contains duplicate requested model %q", svc.Name, requestedModel)
			}
			serviceRequestedModels[requestedModel] = struct{}{}

			if len(svc.ClientKeys) == 0 {
				continue
			}
			for _, clientKey := range svc.ClientKeys {
				if _, ok := keyModelOwners[clientKey]; !ok {
					keyModelOwners[clientKey] = map[string]string{}
				}
				ownerKey := svc.UpstreamProtocol + ":" + requestedModel
				if owner, ok := keyModelOwners[clientKey][ownerKey]; ok && owner != svc.Name {
					return fmt.Errorf("model %q is assigned to multiple upstream services for client key %q and protocol %q", requestedModel, clientKey, svc.UpstreamProtocol)
				}
				keyModelOwners[clientKey][ownerKey] = svc.Name
			}
		}
	}

	if defaultCount == 0 {
		return errors.New("must have exactly one default upstream service")
	}
	if defaultCount > 1 {
		return errors.New("only one upstream service can be marked as default")
	}
	if !c.Features.KeyPassthrough && len(c.ClientKeys()) == 0 {
		return errors.New("at least one upstream_services.client_keys entry is required when key_passthrough is disabled")
	}
	return nil
}

func (c *AppConfig) ApplyServerEnv(env *ServerEnv) {
	if env == nil {
		return
	}
	c.Server.Port = env.Port
	c.Server.Host = env.Host
	c.Server.Timeout = env.Timeout
}

func (c *AppConfig) BuildRoutingTable() (*RoutingTable, error) {
	table := &RoutingTable{
		RequestedModelToRoutes: make(map[string][]ModelRoute),
		AliasToModels:          make(map[string][]string),
		KeyToServices:          make(map[string][]UpstreamService),
	}

	for _, svc := range c.UpstreamServices {
		if svc.IsDefault {
			table.DefaultService = svc
		}
		for _, clientKey := range svc.ClientKeys {
			table.KeyToServices[clientKey] = append(table.KeyToServices[clientKey], svc)
		}
		for _, modelEntry := range svc.Models {
			route := ModelRoute{
				Service:        svc,
				ServiceName:    svc.Name,
				RequestedModel: modelEntry,
				ActualModel:    modelEntry,
				ModelEntry:     modelEntry,
			}
			if strings.Contains(modelEntry, ":") {
				parts := strings.SplitN(modelEntry, ":", 2)
				alias := strings.TrimSpace(parts[0])
				actualModel := strings.TrimSpace(parts[1])
				route.RequestedModel = alias
				route.ActualModel = actualModel
				table.AliasToModels[alias] = append(table.AliasToModels[alias], modelEntry)
			}
			table.RequestedModelToRoutes[route.RequestedModel] = append(table.RequestedModelToRoutes[route.RequestedModel], route)
		}
	}

	if table.DefaultService.Name == "" {
		return nil, errors.New("no default service configured")
	}
	return table, nil
}

func containsAnyPlaceholder(template string, placeholders []string) bool {
	for _, placeholder := range placeholders {
		if strings.Contains(template, placeholder) {
			return true
		}
	}
	return false
}

func validatePromptTemplate(template string, fieldName string) error {
	if strings.TrimSpace(template) == "" {
		return nil
	}
	if !containsAnyPlaceholder(template, promptToolCatalogPlaceholders) || !containsAnyPlaceholder(template, promptProtocolPlaceholders) {
		return fmt.Errorf("%s must contain the tool catalog placeholder {tool_catalog} and one protocol placeholder ({trigger_signal}, {protocol_rules}, {single_call_example}, or {multi_call_example})", fieldName)
	}
	return nil
}

func (c *AppConfig) ClientKeys() []string {
	seen := map[string]struct{}{}
	keys := make([]string, 0)
	for _, svc := range c.UpstreamServices {
		for _, key := range svc.ClientKeys {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	return keys
}

func normalizeStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (s ServerConfig) TimeoutDuration() time.Duration {
	return time.Duration(s.Timeout) * time.Second
}

func (s ServerConfig) PortString() string {
	return strconv.Itoa(s.Port)
}
