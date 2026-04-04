package bestiary

// staticModels is declared and populated in the generated models_static_gen.go.
// It is referenced here by the registry query functions below.

// StaticModels returns a defensive copy of the compiled-in model data.
// Modifying the returned slice does not affect the registry.
func StaticModels() []ModelInfo {
	out := make([]ModelInfo, len(staticModels))
	copy(out, staticModels)
	return out
}

// LookupModel searches the static registry for a model by its ID.
// It returns the model and true if found, or the zero value and false otherwise.
func LookupModel(id ModelID) (ModelInfo, bool) {
	for _, m := range staticModels {
		if m.ID == id {
			return m, true
		}
	}
	return ModelInfo{}, false
}

// ModelsByProvider returns all static models from the given provider.
func ModelsByProvider(p Provider) []ModelInfo {
	var out []ModelInfo
	for _, m := range staticModels {
		if m.Provider == p {
			out = append(out, m)
		}
	}
	return out
}

// ModelsByFamily returns all static models belonging to the given family.
func ModelsByFamily(family string) []ModelInfo {
	var out []ModelInfo
	for _, m := range staticModels {
		if m.Family == family {
			out = append(out, m)
		}
	}
	return out
}
