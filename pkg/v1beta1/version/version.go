package version

import "fmt"

const Version = "v1beta1"
const Namespace = "kanopy-platform.github.io"

func String() string {
	return fmt.Sprintf("%s.%s", Version, Namespace)
}
