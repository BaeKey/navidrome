package public

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/utils/req"
)

func (pub *Router) handleImages(w http.ResponseWriter, r *http.Request) {
	// If context is already canceled, discard request without further processing
	if r.Context().Err() != nil {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	p := req.Params(r)
	id, _ := p.String(":id")
	if id == "" {
		log.Warn(r, "No id provided")
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	artId, err := decodeArtworkID(id)
	if err != nil {
		log.Error(r, "Error decoding artwork id", "id", id, err)
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	size := p.IntOr("size", 0)
	square := p.BoolOr("square", false)

	imgReader, lastUpdate, err := pub.artwork.Get(ctx, artId, size, square)
	switch {
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, model.ErrNotFound):
		log.Warn(r, "Couldn't find coverArt", "id", id, err)
		http.Error(w, "Artwork not found", http.StatusNotFound)
		return
	case errors.Is(err, artwork.ErrUnavailable):
		log.Debug(r, "Item does not have artwork", "id", id, err)
		http.Error(w, "Artwork not found", http.StatusNotFound)
		return
	case err != nil:
		log.Error(r, "Error retrieving coverArt", "id", id, err)
		http.Error(w, "Error retrieving coverArt", http.StatusInternalServerError)
		return
	}

	defer imgReader.Close()
	if ar, ok := imgReader.(interface {
		AccelRedirect(prefix string) (string, bool)
	}); ok {
		if redirect, ok := ar.AccelRedirect(conf.Server.CacheAccelRedirectPrefix); ok {
			w.Header().Set("Cache-Control", "public, max-age=315360000")
			w.Header().Set("Last-Modified", lastUpdate.Format(http.TimeFormat))
			if contentType := detectImageContentType(imgReader); contentType != "" {
				w.Header().Set("Content-Type", contentType)
			}
			w.Header().Set("X-Accel-Redirect", redirect)
			return
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=315360000")
	w.Header().Set("Last-Modified", lastUpdate.Format(http.TimeFormat))
	cnt, err := io.Copy(w, imgReader)
	if err != nil {
		log.Warn(ctx, "Error sending image", "count", cnt, err)
	}
}

func detectImageContentType(r io.Reader) string {
	var buf [512]byte
	n, err := r.Read(buf[:])
	if seeker, ok := r.(io.Seeker); ok {
		_, _ = seeker.Seek(0, io.SeekStart)
	}
	if n == 0 || (err != nil && err != io.EOF) {
		return ""
	}
	contentType := http.DetectContentType(buf[:n])
	if contentType == "application/octet-stream" && n >= 12 && string(buf[0:4]) == "RIFF" && string(buf[8:12]) == "WEBP" {
		return "image/webp"
	}
	return contentType
}

func decodeArtworkID(tokenString string) (model.ArtworkID, error) {
	token, err := auth.TokenAuth.Decode(tokenString)
	if err != nil {
		return model.ArtworkID{}, err
	}
	if token == nil {
		return model.ArtworkID{}, errors.New("unauthorized")
	}
	c := auth.ClaimsFromToken(token)
	if c.ID == "" {
		return model.ArtworkID{}, errors.New("required claim \"id\" not found")
	}
	artID, err := model.ParseArtworkID(c.ID)
	if err == nil {
		return artID, nil
	}
	// Try to default to mediafile artworkId (if used with a mediafileShare token)
	return model.ParseArtworkID("mf-" + c.ID)
}
