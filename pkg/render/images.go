package render

import (
	"fmt"

	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/cluster"
	"sigs.k8s.io/yaml"
)

// renderImages builds the Kubernetes manifests produced by a collection
// of images. Image indexes are preserved in generated filenames.
func renderImages(images cluster.Images, name string) (*spec.RenderPhase, error) {
	phase := &spec.RenderPhase{}

	images = images.ResolveDefaultNames(name)

	for i, image := range images {
		if image.Deploy == nil {
			continue
		}

		manifests, err := image.Deploy.Manifests(image.Image)
		if err != nil {
			return nil, fmt.Errorf("render image %d: %w", i, err)
		}

		if manifests == nil {
			continue
		}

		for j, secret := range manifests.Secrets {
			if err := addManifest(phase, fmt.Sprintf("%02d-secret-%02d.yaml", i, j), secret); err != nil {
				return nil, fmt.Errorf("render image %d Secret %d: %w", i, j, err)
			}
		}

		if manifests.Deployment != nil {
			if err := addManifest(phase, fmt.Sprintf("%02d-deployment.yaml", i), manifests.Deployment); err != nil {
				return nil, fmt.Errorf("render image %d Deployment: %w", i, err)
			}
		}

		if manifests.Service != nil {
			if err := addManifest(phase, fmt.Sprintf("%02d-service.yaml", i), manifests.Service); err != nil {
				return nil, fmt.Errorf("render image %d Service: %w", i, err)
			}
		}
	}

	return phase, nil
}

// addManifest serializes a Kubernetes resource as YAML and adds it
// to the supplied render phase.
func addManifest(phase *spec.RenderPhase, path string, manifest any) error {
	content, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}

	phase.Files = append(phase.Files, spec.RenderFile{
		Path:    path,
		Content: content,
		YAML:    true,
	})

	return nil
}
