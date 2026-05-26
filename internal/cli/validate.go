package cli

import (
	"fmt"

	"jeju/internal/config"
)

func runValidate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: jeju validate <agent.yaml>")
	}
	manifest, _, err := config.LoadFile(args[0])
	if err != nil {
		return err
	}
	if err := config.Validate(manifest); err != nil {
		return err
	}
	fmt.Printf("valid: %s\n", args[0])
	return nil
}
