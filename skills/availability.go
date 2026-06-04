package skills

var blocked = map[string]bool{
	"katana":  true,
	"passive": true,
}

func enableSkill(name string) {
	delete(blocked, name)
}

func skillAvailable(name string) bool {
	return !blocked[name]
}
