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

// ModelsByFamily returns all static models with the given raw API family string.
// The family parameter matches the RawFamily field (verbatim API value, e.g.
// "claude-opus", "gemini-flash").
func ModelsByFamily(family Family) []ModelInfo {
	var out []ModelInfo
	for _, m := range staticModels {
		if m.RawFamily == family {
			out = append(out, m)
		}
	}
	return out
}

// LookupModelByProvider searches the static registry for a model matching both
// the given provider and name (model ID string). It returns the model and true
// if found, or the zero value and false otherwise.
func LookupModelByProvider(p Provider, name string) (ModelInfo, bool) {
	for _, m := range staticModels {
		if m.Provider == p && string(m.ID) == name {
			return m, true
		}
	}
	return ModelInfo{}, false
}

// Models returns all available models. It delegates to StaticModels and returns
// a defensive copy so callers cannot mutate the registry. This is the preferred
// API for external callers; StaticModels is an implementation detail.
//
// See ModelIDs() (in models_constants_gen.go) for the canonical Model_* constant slice.
func Models() []ModelInfo {
	return StaticModels()
}
