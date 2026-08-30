//go:build forgespec

package model

import "cleanfixture/markers"

//forge:collection sort=Age index=Name
//forge:ring cap=1024 overflow=overwrite
type Persons markers.Collection[markers.Ring[markers.Json[Person]]]
