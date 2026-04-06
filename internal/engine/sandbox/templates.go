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
		LaunchOpts:    "-novid -nojoy -low -w 640 -h 480 +fps_max 10 +cl_disablehtmlmotd 1 -nosound -nopreload +r_dynamic 0 +mat_queue_mode 0 +game_type 1 +game_mode 2 +map de_dust2 +bot_quota 9 +bot_difficulty 3 -condebug",
		CPULimit:      1_000_000_000,
		MemoryLimitMB: 2560,
		GPURequired:   true,
	},
	"dota2": {
		Image:         "sfarm-dota2:latest",
		LaunchOpts:    "-novid -nojoy -low -w 640 -h 480 +fps_max 10 -map dota -nosound -nopreload",
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
