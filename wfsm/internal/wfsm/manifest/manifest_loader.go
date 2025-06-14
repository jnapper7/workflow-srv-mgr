// Copyright AGNTCY Contributors (https://github.com/agntcy)
// SPDX-License-Identifier: Apache-2.0
package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	coretypes "github.com/agntcy/dir/api/core/v1alpha1"
	hubClient "github.com/agntcy/dir/hub/client/hub"

	"github.com/agntcy/dir/client"
	"github.com/agntcy/dir/hub/api/v1alpha1"
	"github.com/cisco-eti/wfsm/manifests"
	"google.golang.org/grpc/metadata"
)

const AgentExtensionName = "schema.oasf.agntcy.org/features/runtime/manifest"

type fileManifestLoader struct {
	filePath string
}

type hubManifestLoader struct {
	accessToken string
	digest      string
	host        string
}

type directoryManifestLoader struct {
	digest       string
	directoryURL string
}

type httpManifestLoader struct {
	url string
}

func LoaderFactory(path string) (ManifestLoader, error) {
	u, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("invalid manifest path: %s", err)
	}

	switch u.Scheme {
	case "http", "https":
		return &httpManifestLoader{
			url: path,
		}, nil
	case "file", "":
		return &fileManifestLoader{
			filePath: strings.TrimPrefix(path, "file://"),
		}, nil
	case "hub":
		accessToken := os.Getenv("ACCESS_TOKEN")
		if accessToken == "" {
			return nil, fmt.Errorf("access token is not set")
		}
		return &hubManifestLoader{
			accessToken: accessToken,
			digest:      strings.TrimPrefix(u.Path, "/"),
			host:        u.Host,
		}, nil
	case "sha256":
		directoryURL := os.Getenv("DIRECTORY_URL")
		if directoryURL == "" {
			directoryURL = client.DefaultServerAddress
		}
		return &directoryManifestLoader{
			digest:       path,
			directoryURL: directoryURL,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported manifest location: %s", path)
	}
}

func (f *fileManifestLoader) loadManifest(ctx context.Context) (manifests.AgentManifest, error) {
	file, err := os.Open(f.filePath)
	if err != nil {
		log.Fatalf("failed to open file: %s", err)
	}
	defer file.Close()

	// Read the file into a byte slice
	byteSlice, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("failed to read file: %s", err)
	}
	agentManifest, err := processOASFManifest(byteSlice)
	if err != nil {
		return manifests.AgentManifest{}, fmt.Errorf("failed to process OASF manifest: %s", err)
	}
	return agentManifest, nil
}

func (l *hubManifestLoader) loadManifest(ctx context.Context) (manifests.AgentManifest, error) {
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+l.accessToken))
	hc, err := hubClient.New(l.host)
	agentID := &v1alpha1.AgentIdentifier{
		Id: &v1alpha1.AgentIdentifier_Digest{
			Digest: l.digest,
		},
	}

	dirManifest, err := hc.PullAgent(ctx, &v1alpha1.PullAgentRequest{
		Id: agentID,
	})
	if err != nil {
		return manifests.AgentManifest{}, fmt.Errorf("failed to pull agent: %v", err)
	}

	agentManifest, err := processOASFManifest(dirManifest)
	if err != nil {
		return manifests.AgentManifest{}, fmt.Errorf("failed to process directory manifest: %s", err)
	}
	return agentManifest, nil
}

func (l *directoryManifestLoader) loadManifest(ctx context.Context) (manifests.AgentManifest, error) {
	dirClient, err := client.New(client.WithConfig(&client.Config{
		ServerAddress: l.directoryURL,
	}))
	if err != nil {
		return manifests.AgentManifest{}, fmt.Errorf("failed to create directory client: %s", err)
	}
	reader, err := dirClient.Pull(ctx, &coretypes.ObjectRef{
		Digest:      l.digest,
		Type:        coretypes.ObjectType_OBJECT_TYPE_AGENT.String(),
		Annotations: nil,
	})
	if err != nil {
		return manifests.AgentManifest{}, fmt.Errorf("failed to pull manifest from directory: %s", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return manifests.AgentManifest{}, fmt.Errorf("failed to read data from reader: %s", err)
	}
	agentManifest, err := processOASFManifest(data)
	if err != nil {
		return manifests.AgentManifest{}, fmt.Errorf("failed to process directory manifest: %s", err)
	}

	return agentManifest, nil
}

func (l *httpManifestLoader) loadManifest(ctx context.Context) (manifests.AgentManifest, error) {
	resp, err := http.Get(l.url)
	if err != nil {
		return manifests.AgentManifest{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return manifests.AgentManifest{}, fmt.Errorf("failed to fetch manifest: %s", resp.Status)
	}
	byteSlice, err := io.ReadAll(resp.Body)
	if err != nil {
		return manifests.AgentManifest{}, fmt.Errorf("failed to read response body: %s", err)
	}

	agentManifest, err := processOASFManifest(byteSlice)
	if err != nil {
		return manifests.AgentManifest{}, fmt.Errorf("failed to process directory manifest: %s", err)
	}
	return agentManifest, nil
}

func processOASFManifest(OASFManifestRaw []byte) (manifests.AgentManifest, error) {

	var agentManifest manifests.AgentManifest
	err := json.Unmarshal(OASFManifestRaw, &agentManifest)
	if err != nil {
		return manifests.AgentManifest{}, fmt.Errorf("failed to unmarshal agent manifest: %s", err)
	}
	return agentManifest, nil
}
