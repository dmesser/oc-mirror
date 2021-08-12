package bundle

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"

	semver "github.com/blang/semver/v4"
	"github.com/google/uuid"
	"github.com/openshift/oc/pkg/cli/admin/release"
	"github.com/sirupsen/logrus"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/RedHatGov/bundle/pkg/config/v1alpha1"
)

// import(
//   "github.com/openshift/cluster-version-operator/pkg/cincinnati"
// )

const (
	UpdateUrl string = "https://api.openshift.com/api/upgrades_info/v1/graph"
)

// This file is for managing OCP release related tasks

const updateUrl string = "https://api.openshift.com/api/upgrades_info/v1/graph"

func getTLSConfig() (*tls.Config, error) {
	certPool, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}

	config := &tls.Config{
		RootCAs: certPool,
	}

	return config, nil
}

func newClient() (Client, *url.URL, error) {
	upstream, err := url.Parse(updateUrl)
	if err != nil {
		return Client{}, nil, err
	}

	// This needs to handle user input at some point.
	var proxy *url.URL

	tls, err := getTLSConfig()
	if err != nil {
		return Client{}, nil, err
	}
	return NewClient(uuid.New(), proxy, tls), upstream, nil
}

// Next calculate the upgrade path from the current version to the channel's latest
func calculateUpgradePath(ch v1alpha1.ReleaseChannel, v semver.Version) (Update, []Update, error) {

	client, upstream, err := newClient()
	if err != nil {
		return Update{}, nil, err
	}

	ctx := context.Background()

	// Does not currently handle arch selection
	arch := "x86_64"

	channel := ch.Name

	upgrade, upgrades, err := client.GetUpdates(ctx, upstream, arch, channel, v)
	if err != nil {
		return Update{}, nil, err
	}

	return upgrade, upgrades, nil
	//	// get latest channel version dowloaded

	//	// output a channel (struct) with the upgrade versions needed
	//
}

//func downloadRelease(c channel) error {
//	// Download the referneced versions by channel
//}
func GetLatestVersion(ch v1alpha1.ReleaseChannel) (Update, error) {

	client, upstream, err := newClient()
	if err != nil {
		return Update{}, err
	}

	ctx := context.Background()

	// Does not currently handle arch selection
	arch := "x86_64"

	channel := ch.Name

	latest, err := client.GetChannelLatest(ctx, upstream, arch, channel)
	if err != nil {
		return Update{}, err
	}
	upgrade, _, err := client.GetUpdates(ctx, upstream, arch, channel, latest)
	if err != nil {
		return Update{}, err
	}

	return upgrade, err
	//	// get latest channel version dowloaded

	//	// output a channel (struct) with the upgrade versions needed
	//
}

func (c Client) GetChannelLatest(ctx context.Context, uri *url.URL, arch string, channel string) (semver.Version, error) {
	var latest Update
	transport := http.Transport{}
	// Prepare parametrized cincinnati query.
	queryParams := uri.Query()
	//queryParams.Add("arch", arch)
	queryParams.Add("channel", channel)
	queryParams.Add("id", c.id.String())
	uri.RawQuery = queryParams.Encode()

	// Download the update graph.
	req, err := http.NewRequest("GET", uri.String(), nil)
	if err != nil {
		return latest.Version, &Error{Reason: "InvalidRequest", Message: err.Error(), cause: err}
	}
	req.Header.Add("Accept", GraphMediaType)
	if c.tlsConfig != nil {
		transport.TLSClientConfig = c.tlsConfig
	}

	if c.proxyURL != nil {
		transport.Proxy = http.ProxyURL(c.proxyURL)
	}

	client := http.Client{Transport: &transport}
	timeoutCtx, cancel := context.WithTimeout(ctx, getUpdatesTimeout)
	defer cancel()
	resp, err := client.Do(req.WithContext(timeoutCtx))
	if err != nil {
		return latest.Version, &Error{Reason: "RemoteFailed", Message: err.Error(), cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return latest.Version, &Error{Reason: "ResponseFailed", Message: fmt.Sprintf("unexpected HTTP status: %s", resp.Status)}
	}

	// Parse the graph.
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return latest.Version, &Error{Reason: "ResponseFailed", Message: err.Error(), cause: err}
	}

	var graph graph
	if err = json.Unmarshal(body, &graph); err != nil {
		return latest.Version, &Error{Reason: "ResponseInvalid", Message: err.Error(), cause: err}
	}

	// Find the current version within the graph.
	Vers := []semver.Version{}
	for _, node := range graph.Nodes {

		Vers = append(Vers, node.Version)
	}

	semver.Sort(Vers)
	new := Vers[len(Vers)-1]

	return new, err
}

func downloadMirror(i string, rootDir string) error {
	stream := genericclioptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}
	opts := release.NewMirrorOptions(stream)

	opts.From = i
	opts.ToDir = rootDir

	if err := opts.Run(); err != nil {
		return err
	}
	return nil

}

func GetReleasesInitial(cfg v1alpha1.ImageSetConfiguration, rootDir string) error {

	// For each channel in the config file
	for _, ch := range cfg.Mirror.OCP.Channels {

		if len(ch.Versions) == 0 {
			// If no version was specified from the channel, then get the latest release
			latest, err := GetLatestVersion(ch)
			if err != nil {
				logrus.Errorln(err)
				return err
			}
			logrus.Infof("Image to download: %v", latest.Image)
			// Download the release
			err = downloadMirror(latest.Image, rootDir)
			if err != nil {
				logrus.Errorln(err)
			}
			logrus.Infof("Channel Latest version %v", latest.Version)
		}

		// Check for specific version declarations for each specific version
		for _, v := range ch.Versions {

			// Convert the string to a semver
			ver, err := semver.Parse(v)

			if err != nil {
				return err
			}

			// This dumps the available upgrades from the last downloaded version
			requested, _, err := calculateUpgradePath(ch, ver)
			if err != nil {
				return fmt.Errorf("failed to get upgrade graph: %v", err)
			}

			logrus.Infof("requested: %v", requested.Version)
			err = downloadMirror(requested.Image, rootDir)
			if err != nil {
				return err
			}
			logrus.Infof("Channel Latest version %v", requested.Version)

			/* Select the requested version from the available versions
			for _, d := range neededVersions {
				logrus.Infof("Available Release Version: %v \n Requested Version: %o", d.Version, rs)
				if d.Version.Equals(rs) {
					logrus.Infof("Image to download: %v", d.Image)
					err := downloadMirror(d.Image)
					if err != nil {
						logrus.Errorln(err)
					}
					logrus.Infof("Image to download: %v", d.Image)
					break
				}
			} */

			// download the selected version

			logrus.Infof("Current Object: %v", v)
			//logrus.Infof("Next-Versions: %v", neededVersions.)
			//nv = append(nv, neededVersions)
		}
	}

	// Download each referenced version from
	//downloadRelease(nv)

	return nil
}

func GetReleasesDiff(_ v1alpha1.PastMirror, c v1alpha1.ImageSetConfiguration, rootDir string) error {

	for _, ch := range c.Mirror.OCP.Channels {
		// Check for specific version declarations for each specific version
		for _, v := range ch.Versions {

			// Convert the string to a semver
			ver, err := semver.Parse(v)

			if err != nil {
				return err
			}

			// This dumps the available upgrades from the last downloaded version
			requested, _, err := calculateUpgradePath(ch, ver)
			if err != nil {
				return fmt.Errorf("failed to get upgrade graph: %v", err)
			}

			logrus.Infof("requested: %v", requested.Version)
			err = downloadMirror(requested.Image, rootDir)
			if err != nil {
				return err
			}
			logrus.Infof("Channel Latest version %v", requested.Version)

			/* Select the requested version from the available versions
			for _, d := range neededVersions {
				logrus.Infof("Available Release Version: %v \n Requested Version: %o", d.Version, rs)
				if d.Version.Equals(rs) {
					logrus.Infof("Image to download: %v", d.Image)
					err := downloadMirror(d.Image)
					if err != nil {
						logrus.Errorln(err)
					}
					logrus.Infof("Image to download: %v", d.Image)
					break
				}
			} */

			// download the selected version

			logrus.Infof("Current Object: %v", v)
			//logrus.Infof("Next-Versions: %v", neededVersions.)
			//nv = append(nv, neededVersions
		}
	}

	// Download each referenced version from
	//downloadRelease(nv)

	return nil
}