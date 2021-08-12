package bundle

import (
	"fmt"
	"os"

	"github.com/openshift/oc/pkg/cli/image/imagesource"
	"github.com/openshift/oc/pkg/cli/image/mirror"
	"github.com/sirupsen/logrus"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/RedHatGov/bundle/pkg/config/v1alpha1"
)

type AdditionalOptions struct {
	DestDir string
	DryRun  bool
	SkipTLS bool
}

func NewAdditionalOptions() *AdditionalOptions {
	return &AdditionalOptions{}
}

// GetAdditional downloads specified images in the imageset-config.yaml under mirror.additonalImages
func (o *AdditionalOptions) GetAdditional(_ v1alpha1.PastMirror, cfg v1alpha1.ImageSetConfiguration) error {

	var mappings []mirror.Mapping

	stream := genericclioptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}

	opts := mirror.NewMirrorImageOptions(stream)
	opts.DryRun = o.DryRun
	opts.SecurityOptions.Insecure = o.SkipTLS
	opts.FileDir = o.DestDir

	logrus.Infof("Downloading %d image(s) to %s", len(cfg.Mirror.AdditionalImages), opts.FileDir)

	for _, img := range cfg.Mirror.AdditionalImages {

		// Get source image information
		srcRef, err := imagesource.ParseReference(img.Name)

		if err != nil {
			return fmt.Errorf("error parsing source image %s: %v", img.Name, err)
		}

		// Set destination image information
		path := "file://" + img.Name

		dstRef, err := imagesource.ParseReference(path)

		if err != nil {
			return fmt.Errorf("error parsing destination reference %s: %v", path, err)
		}

		// Check if image is specified as a blocked image
		if IsBlocked(cfg, srcRef.Ref) {
			return fmt.Errorf("additional image %s also specified as blocked, remove the image one config field or the other", img.Name)
		}
		// Create mapping from source and destination images
		mappings = append(mappings, mirror.Mapping{
			Source:      srcRef,
			Destination: dstRef,
			Name:        srcRef.Ref.Name,
		})
	}

	opts.Mappings = mappings

	err := opts.Run()
	return err
}