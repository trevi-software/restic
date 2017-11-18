package repository

import (
	"encoding/json"
	"io"

	"github.com/restic/restic/internal/debug"
	"github.com/restic/restic/internal/errors"
	"github.com/restic/restic/internal/restic"
)

// loads an Index from a stream of JSON text. Index is built gradually so that the entire JSON string
// does not need to be read into RAM all at once.
type indexJsonStreamer struct {
	rd      io.Reader
	idxJson *jsonIndex
	dec     *json.Decoder
	token   json.Token
	err     error
}

func NewJsonStreamer(rd io.Reader) *indexJsonStreamer {
	return &indexJsonStreamer{
		rd:      rd,
		idxJson: &jsonIndex{},
		dec:     json.NewDecoder(rd)}
}

// build an Index gradually by processing one token at a time from the underlying json stream.
func (j *indexJsonStreamer) LoadIndex() (*jsonIndex, error) {
	debug.Log("Start decoding index streaming")

	// opening bracket
	j.readBracket()

	for j.hasMore() {
		j.readToken()

		switch j.token {
		case "supersedes":
			// opening bracket
			j.readBracket()

			var supercedes restic.IDs

			for j.hasMore() {
				var id restic.ID
				j.decodeNextValue(&id)
				supercedes = append(supercedes, id)
			}
			j.idxJson.Supersedes = supercedes

			// close bracket
			j.readBracket()

		case "packs":
			// opening bracket
			j.readBracket()

			for j.hasMore() {
				var pack packJSON
				j.decodeNextValue(&pack)
				j.idxJson.Packs = append(j.idxJson.Packs, &pack)
			}

			// close bracket
			j.readBracket()

		default:
			return nil, j.err
		}
	}

	// closing bracket
	j.readBracket()

	return j.idxJson, j.err
}

func (j *indexJsonStreamer) readBracket() {
	if j.err != nil {
		return
	}

	t, err := j.dec.Token()

	if err != nil {
		j.err = errors.Wrapf(err, "%+v, expected bracket: %v", err, t)
	}

	j.token = t
}

// next token should be either "supersedes" or "packs"
func (j *indexJsonStreamer) readToken() {
	if j.err != nil {
		return
	}

	t, err := j.dec.Token()

	if err != nil {
		j.err = errors.Wrapf(err, "%+v, token: %v (expected \"supersedes\" or \"packs\"", err, t)
	}

	j.token = t
}

func (j *indexJsonStreamer) decodeNextValue(d interface{}) {
	if j.err != nil {
		return
	}

	err := j.dec.Decode(d)
	if err != nil {
		j.err = err
	}
}

func (j *indexJsonStreamer) hasMore() bool {
	return j.err == nil && j.dec.More()
}
