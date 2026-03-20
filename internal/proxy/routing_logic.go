package proxy

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"x-tool/internal/config"
)

func buildRoutingTableOrEmpty(cfg *config.AppConfig) (*config.RoutingTable, error) {
	if cfg == nil {
		return nil, errors.New("configuration is nil")
	}
	if len(cfg.UpstreamServices) == 0 {
		return &config.RoutingTable{
			RequestedModelToRoutes: make(map[string][]config.ModelRoute),
			AliasToModels:          make(map[string][]string),
			KeyToServices:          make(map[string][]config.UpstreamService),
		}, nil
	}
	return cfg.BuildRoutingTable()
}

func (a *App) findUpstream(clientKey, modelName string) (config.UpstreamService, string, error) {
	return a.findUpstreamForProtocol(clientKey, modelName, config.UpstreamProtocolOpenAICompat)
}

func (a *App) findUpstreamForProtocol(clientKey, modelName, protocolName string) (config.UpstreamService, string, error) {
	if len(a.Config().UpstreamServices) == 0 {
		return config.UpstreamService{}, "", errNoUpstreamConfigured
	}

	services := a.servicesForClientKeyAndProtocol(clientKey, protocolName)
	if len(services) == 0 {
		return config.UpstreamService{}, "", errModelNotAccessible
	}

	if a.Config().Features.ModelPassthrough {
		service := services[0]
		if strings.TrimSpace(service.APIKey) == "" && !a.Config().Features.KeyPassthrough {
			return config.UpstreamService{}, "", errors.New("configuration error: API key not found for the 'openai' service in model passthrough mode")
		}
		return service, modelName, nil
	}

	allowedServices := make(map[string]config.UpstreamService, len(services))
	for _, service := range services {
		allowedServices[service.Name] = service
	}

	if routes := a.Routing().RequestedModelToRoutes[modelName]; len(routes) > 0 {
		matches := make([]config.ModelRoute, 0, len(routes))
		for _, route := range routes {
			if _, ok := allowedServices[route.ServiceName]; ok {
				matches = append(matches, route)
			}
		}
		if len(matches) == 1 {
			service := matches[0].Service
			if strings.TrimSpace(service.APIKey) == "" && !a.Config().Features.KeyPassthrough {
				return config.UpstreamService{}, "", fmt.Errorf("model configuration error: API key not found for service %q", service.Name)
			}
			return service, matches[0].ActualModel, nil
		}
		if len(matches) > 1 {
			// Prefer same-protocol match over openai_compat
			for _, route := range matches {
				if route.Service.UpstreamProtocol == protocolName {
					service := route.Service
					if strings.TrimSpace(service.APIKey) == "" && !a.Config().Features.KeyPassthrough {
						return config.UpstreamService{}, "", fmt.Errorf("model configuration error: API key not found for service %q", service.Name)
					}
					return service, route.ActualModel, nil
				}
			}
			for _, route := range matches {
				if route.ServiceName != a.Routing().DefaultService.Name {
					continue
				}
				service := route.Service
				if strings.TrimSpace(service.APIKey) == "" && !a.Config().Features.KeyPassthrough {
					return config.UpstreamService{}, "", fmt.Errorf("model configuration error: API key not found for service %q", service.Name)
				}
				return service, route.ActualModel, nil
			}
			return config.UpstreamService{}, "", fmt.Errorf("configuration error: model %q resolves to multiple upstream services for this client key", modelName)
		}
	}

	if len(services) == 1 {
		service := services[0]
		if strings.TrimSpace(service.APIKey) == "" && !a.Config().Features.KeyPassthrough {
			return config.UpstreamService{}, "", fmt.Errorf("model configuration error: API key not found for service %q", service.Name)
		}
		return service, modelName, nil
	}

	if service, ok := allowedServices[a.Routing().DefaultService.Name]; ok {
		if strings.TrimSpace(service.APIKey) == "" && !a.Config().Features.KeyPassthrough {
			return config.UpstreamService{}, "", fmt.Errorf("model configuration error: API key not found for service %q", service.Name)
		}
		return service, modelName, nil
	}

	return config.UpstreamService{}, "", errModelNotAccessible
}

func (a *App) visibleModels(clientKey string) []string {
	return a.visibleModelsForProtocol(clientKey, "")
}

func (a *App) visibleModelsForProtocol(clientKey, protocolName string) []string {
	visible := map[string]struct{}{}
	for _, service := range a.servicesForClientKeyAndProtocol(clientKey, protocolName) {
		for _, modelEntry := range service.Models {
			modelName := modelEntry
			if strings.Contains(modelEntry, ":") {
				parts := strings.SplitN(modelEntry, ":", 2)
				modelName = strings.TrimSpace(parts[0])
			}
			visible[modelName] = struct{}{}
		}
	}

	models := make([]string, 0, len(visible))
	for modelName := range visible {
		models = append(models, modelName)
	}
	sort.Strings(models)
	return models
}

func (a *App) servicesForClientKey(clientKey string) []config.UpstreamService {
	if a.Config().Features.KeyPassthrough {
		return append([]config.UpstreamService(nil), a.Config().UpstreamServices...)
	}
	if len(a.Config().UpstreamServices) == 0 {
		return nil
	}
	services := a.Routing().KeyToServices[strings.TrimSpace(clientKey)]
	return append([]config.UpstreamService(nil), services...)
}

func (a *App) servicesForClientKeyAndProtocol(clientKey, protocolName string) []config.UpstreamService {
	services := a.servicesForClientKey(clientKey)
	if strings.TrimSpace(protocolName) == "" {
		return services
	}

	normalizedProtocol, ok := config.NormalizeUpstreamProtocol(protocolName)
	if !ok {
		return nil
	}
	// openai_compat accepts all protocols; others require exact match.
	filtered := make([]config.UpstreamService, 0, len(services))
	for _, service := range services {
		serviceProtocol, _ := config.NormalizeUpstreamProtocol(service.UpstreamProtocol)
		if serviceProtocol == normalizedProtocol || serviceProtocol == config.UpstreamProtocolOpenAICompat {
			filtered = append(filtered, service)
		}
	}
	return filtered
}

func (a *App) aliasSummaries() []map[string]any {
	aliases := make([]string, 0, len(a.Routing().AliasToModels))
	for alias := range a.Routing().AliasToModels {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)

	result := make([]map[string]any, 0, len(aliases))
	for _, alias := range aliases {
		targets := append([]string(nil), a.Routing().AliasToModels[alias]...)
		sort.Strings(targets)
		result = append(result, map[string]any{
			"alias":   alias,
			"targets": targets,
		})
	}
	return result
}
