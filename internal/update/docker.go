package update

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const dockerSock = "/var/run/docker.sock"

func SocketOK() bool {
	if _, err := os.Stat(dockerSock); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := dockerDo(ctx, http.MethodGet, "/_ping", nil)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode < 300
}

func dockerClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Minute,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, "unix", dockerSock)
			},
		},
	}
}

func dockerDo(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return dockerClient().Do(req)
}

type containerSummary struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	Labels map[string]string `json:"Labels"`
}

func RunningDigest(ctx context.Context, image, project string) (string, error) {
	list, err := listProject(ctx, project)
	if err != nil {
		return "", err
	}
	for _, c := range list {
		if !strings.Contains(strings.ToLower(c.Image), "sounddock") {
			continue
		}
		d, err := containerRepoDigest(ctx, c.ID)
		if err == nil && d != "" {
			return d, nil
		}
	}
	_ = image
	return "", nil
}

func containerRepoDigest(ctx context.Context, id string) (string, error) {
	res, err := dockerDo(ctx, http.MethodGet, "/v1.41/containers/"+id+"/json", nil)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("inspect %s", res.Status)
	}
	var info struct {
		Image string `json:"Image"`
	}
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		return "", err
	}
	imgRes, err := dockerDo(ctx, http.MethodGet, "/v1.41/images/"+url.PathEscape(info.Image)+"/json", nil)
	if err != nil {
		return info.Image, nil
	}
	defer imgRes.Body.Close()
	var img struct {
		RepoDigests []string `json:"RepoDigests"`
		ID          string   `json:"Id"`
	}
	_ = json.NewDecoder(imgRes.Body).Decode(&img)
	for _, d := range img.RepoDigests {
		if i := strings.Index(d, "@"); i >= 0 {
			return d[i+1:], nil
		}
	}
	return img.ID, nil
}

func listProject(ctx context.Context, project string) ([]containerSummary, error) {
	q := url.Values{}
	q.Set("all", "1")
	if project != "" {
		b, _ := json.Marshal(map[string][]string{"label": {"com.docker.compose.project=" + project}})
		q.Set("filters", string(b))
	}
	res, err := dockerDo(ctx, http.MethodGet, "/v1.41/containers/json?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("list containers %s", res.Status)
	}
	var out []containerSummary
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out) == 0 && project != "" {
		return listByImage(ctx)
	}
	return out, nil
}

func listByImage(ctx context.Context) ([]containerSummary, error) {
	res, err := dockerDo(ctx, http.MethodGet, "/v1.41/containers/json?all=1", nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var all []containerSummary
	if err := json.NewDecoder(res.Body).Decode(&all); err != nil {
		return nil, err
	}
	var out []containerSummary
	for _, c := range all {
		if strings.Contains(strings.ToLower(c.Image), "sounddock") {
			out = append(out, c)
		}
	}
	return out, nil
}

func PullAndRecreate(ctx context.Context, image, project string) error {
	if !SocketOK() {
		return fmt.Errorf("docker socket is not available inside the container")
	}
	list, err := listProject(ctx, project)
	if err != nil {
		return err
	}
	images := map[string]struct{}{}
	images[image] = struct{}{}
	for _, c := range list {
		if c.Image != "" {
			images[c.Image] = struct{}{}
		}
	}
	for img := range images {
		if err := pullImage(ctx, img); err != nil {
			return fmt.Errorf("pull %s: %w", img, err)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		si := list[i].Labels["com.docker.compose.service"]
		sj := list[j].Labels["com.docker.compose.service"]
		rank := func(s string) int {
			if s == "postgres" {
				return 0
			}
			if s == "sounddock" {
				return 2
			}
			return 1
		}
		return rank(si) < rank(sj)
	})
	for _, c := range list {
		if err := recreate(ctx, c.ID); err != nil {
			return fmt.Errorf("recreate %s: %w", c.ID[:12], err)
		}
	}
	return nil
}

func pullImage(ctx context.Context, ref string) error {
	from, tag := ref, "latest"
	if i := strings.LastIndex(ref, ":"); i > 0 && !strings.Contains(ref[i:], "/") {
		from, tag = ref[:i], ref[i+1:]
	}
	path := "/v1.41/images/create?fromImage=" + url.QueryEscape(from) + "&tag=" + url.QueryEscape(tag)
	res, err := dockerDo(ctx, http.MethodPost, path, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode >= 300 {
		return fmt.Errorf("%s", res.Status)
	}
	return nil
}

func recreate(ctx context.Context, id string) error {
	res, err := dockerDo(ctx, http.MethodGet, "/v1.41/containers/"+id+"/json", nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("inspect %s", res.Status)
	}
	var info struct {
		Name            string          `json:"Name"`
		Config          json.RawMessage `json:"Config"`
		HostConfig      json.RawMessage `json:"HostConfig"`
		NetworkSettings struct {
			Networks map[string]json.RawMessage `json:"Networks"`
		} `json:"NetworkSettings"`
	}
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		return err
	}
	name := strings.TrimPrefix(info.Name, "/")
	tmp := name + "-updating"
	_, _ = dockerDoDiscard(ctx, http.MethodDelete, "/v1.41/containers/"+tmp+"?force=1", nil)

	var top map[string]any
	if err := json.Unmarshal(info.Config, &top); err != nil {
		return err
	}
	top["HostConfig"] = json.RawMessage(info.HostConfig)
	if len(info.NetworkSettings.Networks) > 0 {
		top["NetworkingConfig"] = map[string]any{"EndpointsConfig": info.NetworkSettings.Networks}
	}
	raw, _ := json.Marshal(top)

	cr, err := dockerDo(ctx, http.MethodPost, "/v1.41/containers/create?name="+url.QueryEscape(tmp), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer cr.Body.Close()
	b, _ := io.ReadAll(cr.Body)
	if cr.StatusCode >= 300 {
		return fmt.Errorf("create: %s", strings.TrimSpace(string(b)))
	}
	var created struct {
		ID string `json:"Id"`
	}
	_ = json.Unmarshal(b, &created)
	if created.ID == "" {
		return fmt.Errorf("create returned no id")
	}
	_, _ = dockerDoDiscard(ctx, http.MethodPost, "/v1.41/containers/"+id+"/stop?t=20", nil)
	sr, err := dockerDo(ctx, http.MethodPost, "/v1.41/containers/"+created.ID+"/start", nil)
	if err != nil {
		_, _ = dockerDoDiscard(ctx, http.MethodPost, "/v1.41/containers/"+id+"/start", nil)
		return err
	}
	defer sr.Body.Close()
	if sr.StatusCode >= 300 {
		_, _ = dockerDoDiscard(ctx, http.MethodPost, "/v1.41/containers/"+id+"/start", nil)
		return fmt.Errorf("start new container failed")
	}
	_, _ = dockerDoDiscard(ctx, http.MethodDelete, "/v1.41/containers/"+id+"?force=1", nil)
	rr, err := dockerDo(ctx, http.MethodPost, "/v1.41/containers/"+created.ID+"/rename?name="+url.QueryEscape(name), nil)
	if err == nil {
		defer rr.Body.Close()
	}
	return nil
}

func dockerDoDiscard(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	res, err := dockerDo(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	return res, nil
}
