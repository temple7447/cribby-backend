package storage

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The Cloudinary docs' worked example: params sorted alphabetically, joined as
// k=v&k=v, API secret appended, SHA-1 hex.
func TestSignatureMatchesCloudinarySpec(t *testing.T) {
	c := &Cloudinary{APISecret: "abcd"}
	got := c.sign(map[string]string{"timestamp": "1315060510", "public_id": "sample", "eager": "w_400,h_300,c_pad|w_260,h_200,c_crop"})
	sum := sha1.Sum([]byte("eager=w_400,h_300,c_pad|w_260,h_200,c_crop&public_id=sample&timestamp=1315060510abcd"))
	if got != hex.EncodeToString(sum[:]) {
		t.Fatalf("got %s", got)
	}
}

func TestUploadSendsSignedMultipartAndParsesResponse(t *testing.T) {
	var form map[string][]string
	var fileLen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1_1/mycloud/image/upload" {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = r.ParseMultipartForm(1 << 20)
		form = r.MultipartForm.Value
		if fhs := r.MultipartForm.File["file"]; len(fhs) == 1 {
			fileLen = int(fhs[0].Size)
		}
		_, _ = w.Write([]byte(`{"secure_url":"https://res.cloudinary.com/mycloud/x.jpg","public_id":"cribby/x"}`))
	}))
	defer srv.Close()
	c := NewCloudinary("mycloud", "key123", "secret")
	c.BaseURL = srv.URL

	up, err := c.Upload(context.Background(), []byte("imagebytes"), "", "cribby/avatars/7", TransformAvatar)
	if err != nil {
		t.Fatal(err)
	}
	if up.URL != "https://res.cloudinary.com/mycloud/x.jpg" || up.PublicID != "cribby/x" || fileLen != 10 {
		t.Fatalf("got %+v fileLen=%d", up, fileLen)
	}
	if form["folder"][0] != "cribby/avatars/7" || form["transformation"][0] != TransformAvatar || form["api_key"][0] != "key123" {
		t.Fatalf("form %v", form)
	}
	// signature covers folder+timestamp+transformation (and never file/api_key)
	sum := sha1.Sum([]byte("folder=cribby/avatars/7&timestamp=" + form["timestamp"][0] + "&transformation=" + TransformAvatar + "secret"))
	if form["signature"][0] != hex.EncodeToString(sum[:]) {
		t.Fatalf("bad signature %s", form["signature"][0])
	}
}

func TestUploadByRemoteURLAndDestroy(t *testing.T) {
	var gotFile, gotPublicID, action string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		action = r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if v := r.MultipartForm.Value["file"]; len(v) == 1 {
			gotFile = v[0]
		}
		if v := r.MultipartForm.Value["public_id"]; len(v) == 1 {
			gotPublicID = v[0]
		}
		_, _ = w.Write([]byte(`{"secure_url":"https://u","public_id":"p","result":"ok"}`))
	}))
	defer srv.Close()
	c := NewCloudinary("mycloud", "k", "s")
	c.BaseURL = srv.URL

	if _, err := c.Upload(context.Background(), nil, "https://images.unsplash.com/x.jpg", "f", TransformListing); err != nil || gotFile != "https://images.unsplash.com/x.jpg" {
		t.Fatalf("err=%v file=%q", err, gotFile)
	}
	if err := c.Destroy(context.Background(), "cribby/x"); err != nil || gotPublicID != "cribby/x" || action != "destroy" {
		t.Fatalf("err=%v public_id=%q action=%q", err, gotPublicID, action)
	}
}

func TestCloudinaryErrorsAndNotConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid Signature"}}`))
	}))
	defer srv.Close()
	c := NewCloudinary("mycloud", "k", "s")
	c.BaseURL = srv.URL
	if _, err := c.Upload(context.Background(), []byte("x"), "", "f", "t"); err == nil || !strings.Contains(err.Error(), "Invalid Signature") {
		t.Fatalf("err=%v", err)
	}

	unconfigured := NewCloudinary("", "", "")
	if unconfigured.Configured() {
		t.Fatal("empty cloud name should not be configured")
	}
	if _, err := unconfigured.Upload(context.Background(), []byte("x"), "", "f", "t"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err=%v", err)
	}
}

func TestPlayableVideoURLForcesH264MP4(t *testing.T) {
	cases := map[string]string{
		"https://res.cloudinary.com/c/video/upload/v123/cribby/listings/7/video/abc.mov": "https://res.cloudinary.com/c/video/upload/f_mp4,vc_h264,q_auto/v123/cribby/listings/7/video/abc.mp4",
		"https://res.cloudinary.com/c/video/upload/v1/x/clip.mp4":                        "https://res.cloudinary.com/c/video/upload/f_mp4,vc_h264,q_auto/v1/x/clip.mp4",
		"https://res.cloudinary.com/c/video/upload/v1/x/noext":                           "https://res.cloudinary.com/c/video/upload/f_mp4,vc_h264,q_auto/v1/x/noext.mp4",
		"https://elsewhere.example/clip.mp4":                                             "https://elsewhere.example/clip.mp4", // not Cloudinary: untouched
	}
	for in, want := range cases {
		if got := playableVideoURL(in); got != want {
			t.Errorf("playableVideoURL(%q)\n got  %s\n want %s", in, got, want)
		}
	}
}

func TestUploadVideoUsesVideoEndpointAndDestroyToo(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = w.Write([]byte(`{"secure_url":"https://res.cloudinary.com/mycloud/video/upload/v9/cribby/v.mov","public_id":"cribby/v","duration":42.5}`))
	}))
	defer srv.Close()
	c := NewCloudinary("mycloud", "key", "secret")
	c.BaseURL = srv.URL
	up, err := c.UploadVideo(context.Background(), []byte("vid"), "cribby/listings/1/video")
	if err != nil {
		t.Fatal(err)
	}
	if up.PublicID != "cribby/v" || up.DurationSec != 42.5 || !strings.HasSuffix(up.URL, "/f_mp4,vc_h264,q_auto/v9/cribby/v.mp4") {
		t.Fatalf("got %+v", up)
	}
	if err := c.DestroyVideo(context.Background(), "cribby/v"); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "/v1_1/mycloud/video/upload" || paths[1] != "/v1_1/mycloud/video/destroy" {
		t.Fatalf("paths %v", paths)
	}
}
