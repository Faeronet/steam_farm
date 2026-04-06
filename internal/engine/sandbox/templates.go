package sandbox

type GameTemplate struct {
	Image        string
	LaunchOpts   string
	CPULimit     int64
	MemoryLimitMB int
	GPURequired  bool
}

var Templates = map[string]GameTemplate{
	"cs2": {
		Image:         "sfarm-cs2:latest",
		LaunchOpts:    "-novid -nojoy -low -vulkan -w 640 -h 480 +fps_max 15 +cl_disablehtmlmotd 1 -nosound",
		CPULimit:      1_000_000_000,
		MemoryLimitMB: 2560,
		GPURequired:   true,
	},
	"dota2": {
		Image:         "sfarm-dota2:latest",
		LaunchOpts:    "-novid -nojoy -low -vulkan -w 640 -h 480 +fps_max 15 -map dota -nosound",
		CPULimit:      1_000_000_000,
		MemoryLimitMB: 2560,
		GPURequired:   true,
	},
}

func GetTemplate(gameType string) *GameTemplate {
	t, ok := Templates[gameType]
	if !ok {
		return nil
	}
	return &t
}
