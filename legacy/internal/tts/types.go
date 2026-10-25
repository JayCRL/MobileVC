package tts

type SynthesizeRequest struct {
	Text   string `json:"text"`
	Format string `json:"format,omitempty"`
}

type SynthesizeResult struct {
	ContentType string
	Filename    string
	Audio       []byte
}

type ProviderRequest struct {
	Text   string `json:"text"`
	Format string `json:"format"`
}

type ProviderResult struct {
	ContentType string
	Audio       []byte
}

type HealthStatus struct {
	Provider string `json:"provider"`
	Status   string `json:"status"`
}
