package pkg

import (
	"fmt"
	"github.com/go-git/go-git/v5"
	"gopkg.in/yaml.v3"
	"io"
	"regexp"
	"sigs.k8s.io/kustomize/api/types"
)

type Deployment struct {
	RepositoryName string
	KustomizePath  string
	Images         []string
}

func NewDeployment(cfg DeploymentConfig) *Deployment {
	toRet := &Deployment{
		RepositoryName: cfg.Repository,
		KustomizePath:  cfg.Path,
		Images:         cfg.Images,
	}
	if toRet.KustomizePath == "" {
		toRet.KustomizePath = "kustomization.yaml"
	}

	return toRet
}

func (d Deployment) Apply(worktree *git.Worktree, newTag string) error {
	// Start by reading the kustomization file
	inFile, err := worktree.Filesystem.Open(d.KustomizePath)
	if err != nil {
		return fmt.Errorf("failed to open kustomization file: %w", err)
	}
	defer func() {
		_ = inFile.Close()
	}()
	kustomizationBytes, err := io.ReadAll(inFile)
	if err != nil {
		return fmt.Errorf("failed to read kustomization file: %w", err)
	}

	// Then unmarshal it so that we have a source of truth to work from
	var kustomization types.Kustomization
	err = yaml.Unmarshal(kustomizationBytes, &kustomization)
	if err != nil {
		return fmt.Errorf("failed to decode kustomization file: %w", err)
	}
	// Also convert the bytes to a string
	kustomizationString := string(kustomizationBytes[:])

	// Loop over the deployment's images, replacing their tags
	changeMade := false
	for _, im := range d.Images {
		// Ensure that the image exists in the unmarshalled file first
		if !hasImage(kustomization.Images, im) {
			return fmt.Errorf("kustomization file does not contain image %s", im)
		}
		if newKustomizationString, err := changeTag(kustomizationString, im, newTag); err != nil {
			return fmt.Errorf("failed to replace image %s: %w", im, err)
		} else {
			changeMade = newKustomizationString != kustomizationString
			kustomizationString = newKustomizationString
		}
	}
	if !changeMade {
		return fmt.Errorf("no changes made")
	}

	// Write it back and stage the file for commit
	outFile, err := worktree.Filesystem.Create(d.KustomizePath)
	_, err = outFile.Write([]byte(kustomizationString))
	if err != nil {
		return fmt.Errorf("failed to write kustomization file: %w", err)
	}
	_ = outFile.Close()
	_, err = worktree.Add(d.KustomizePath)
	if err != nil {
		return fmt.Errorf("failed to stage kustomization file: %w", err)
	}

	// Commit the change
	_, err = worktree.Commit("Automated version bump", &git.CommitOptions{})
	if err != nil {
		return fmt.Errorf("failed to commit kustomization file: %w", err)
	}

	return nil
}

func hasImage(images []types.Image, search string) bool {
	for _, image := range images {
		if image.Name == search {
			return true
		}
	}

	return false
}

func changeTag(kustomizeBody string, imageName string, newTag string) (string, error) {
	// To use the image name in the regex, we first have to quote it
	quotedName := regexp.QuoteMeta(imageName)
	// Substitute the name in and compile the regex
	reTpl := `(?ms)^\s*-\s+name:\s+["']?%s["']?$.+?^\s+newTag:\s+["']?([^"'$]+?)["']?$`
	re, err := regexp.Compile(fmt.Sprintf(reTpl, quotedName))
	if err != nil {
		return "", fmt.Errorf("failed to compile regex: %w", err)
	}
	// We only want 1 match, but search for 2 so we can detect duplicates
	matches := re.FindAllStringSubmatchIndex(kustomizeBody, 2)
	if len(matches) == 0 {
		return "", fmt.Errorf("could not find image definition for %s", imageName)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("found more than one image definition for %s", imageName)
	}
	// Finally, use the indexes from the match to construct the name body
	match := matches[0]
	tagStart := match[2]
	tagEnd := match[3]
	return kustomizeBody[0:tagStart] + newTag + kustomizeBody[tagEnd:], nil
}
