package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"sx/backends"
)

func applyStrictEngineWarnings(outcome *backends.SearchOutcome, requested []string) {
	warnings := strictEngineWarnings(requested, outcome.Results, outcome.Diagnostics)
	if len(warnings) == 0 {
		return
	}
	outcome.Warnings = appendUniqueStrings(outcome.Warnings, warnings...)
	if outcome.Diagnostics != nil {
		outcome.Diagnostics.StrictEnginesWarnings = appendUniqueStrings(outcome.Diagnostics.StrictEnginesWarnings, warnings...)
	}
}

func strictEngineWarnings(requested []string, results []SearchResult, diagnostics *backends.SearxngDiagnostics) []string {
	requestedSet := normalizedEngineSet(requested)
	if len(requestedSet) == 0 {
		return nil
	}

	var warnings []string
	for _, engine := range unresponsiveEngineNames(diagnostics) {
		if _, ok := requestedSet[normalizeEngineName(engine)]; ok {
			warnings = append(warnings, fmt.Sprintf("strict-engines: requested SearXNG engine %q was reported unresponsive", engine))
		}
	}

	unrequested := make(map[string]string)
	for _, result := range results {
		for _, engine := range resultEngineNames(result) {
			normalized := normalizeEngineName(engine)
			if normalized == "" {
				continue
			}
			if _, ok := requestedSet[normalized]; !ok {
				unrequested[normalized] = strings.TrimSpace(engine)
			}
		}
	}
	for _, engine := range sortedEngineValues(unrequested) {
		warnings = append(warnings, fmt.Sprintf("strict-engines: results included unrequested SearXNG engine %q", engine))
	}

	return appendUniqueStrings(nil, warnings...)
}

func normalizedEngineSet(engines []string) map[string]struct{} {
	set := make(map[string]struct{}, len(engines))
	for _, engine := range engines {
		normalized := normalizeEngineName(engine)
		if normalized == "" {
			continue
		}
		set[normalized] = struct{}{}
	}
	return set
}

func resultEngineNames(result SearchResult) []string {
	seen := map[string]struct{}{}
	var engines []string
	add := func(engine string) {
		normalized := normalizeEngineName(engine)
		if normalized == "" {
			return
		}
		if _, ok := seen[normalized]; ok {
			return
		}
		seen[normalized] = struct{}{}
		engines = append(engines, strings.TrimSpace(engine))
	}

	add(result.Engine)
	for _, engine := range result.Engines {
		add(engine)
	}
	return engines
}

func unresponsiveEngineNames(diagnostics *backends.SearxngDiagnostics) []string {
	if diagnostics == nil || len(diagnostics.UnresponsiveEngines) == 0 {
		return nil
	}

	var value interface{}
	if err := json.Unmarshal(diagnostics.UnresponsiveEngines, &value); err != nil {
		return nil
	}

	names := map[string]string{}
	add := func(engine string) {
		normalized := normalizeEngineName(engine)
		if normalized == "" {
			return
		}
		names[normalized] = strings.TrimSpace(engine)
	}

	switch v := value.(type) {
	case []interface{}:
		for _, entry := range v {
			switch item := entry.(type) {
			case string:
				add(item)
			case []interface{}:
				if len(item) > 0 {
					if engine, ok := item[0].(string); ok {
						add(engine)
					}
				}
			case map[string]interface{}:
				addEngineFromMap(item, add)
			}
		}
	case map[string]interface{}:
		if !addEngineFromMap(v, add) {
			for key := range v {
				add(key)
			}
		}
	}

	return sortedEngineValues(names)
}

func addEngineFromMap(value map[string]interface{}, add func(string)) bool {
	for _, key := range []string{"engine", "name"} {
		if engine, ok := value[key].(string); ok {
			add(engine)
			return true
		}
	}
	return false
}

func normalizeEngineName(engine string) string {
	return strings.ToLower(strings.TrimSpace(engine))
}

func sortedEngineValues(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, values[key])
	}
	return out
}

func appendUniqueStrings(base []string, extras ...string) []string {
	seen := make(map[string]struct{}, len(base)+len(extras))
	out := make([]string, 0, len(base)+len(extras))
	for _, value := range base {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, value := range extras {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
