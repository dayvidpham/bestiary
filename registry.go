package bestiary

// staticModels is populated by the generated models_static_gen.go file.
var staticModels []ModelInfo

// StaticModels returns a copy of the compiled-in model data.
// Modifying the returned slice does not affect the registry.
func StaticModels() []ModelInfo {
	return nil // stub — implemented in L3
}

// LookupModel searches the static registry for a model by its ID.
// It returns the model and true if found, or the zero value and false otherwise.
func LookupModel(id ModelID) (ModelInfo, bool) {
	return ModelInfo{}, false // stub — implemented in L3
}

// ModelsByProvider returns all static models from the given provider.
func ModelsByProvider(p Provider) []ModelInfo {
	return nil // stub — implemented in L3
}

// ModelsByFamily returns all static models belonging to the given family.
func ModelsByFamily(family string) []ModelInfo {
	return nil // stub — implemented in L3
}
