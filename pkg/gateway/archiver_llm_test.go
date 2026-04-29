package gateway

import (
	"testing"

	"github.com/sipeed/picoclaw/pkg/archiver"
)

func TestArchiverLLMAdapter_SatisfiesInterface(t *testing.T) {
	// The adapter must satisfy archiver.LLMClient.
	var _ archiver.LLMClient = (*archiverLLMAdapter)(nil)
}
