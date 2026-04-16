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
		Image:       "sfarm-cs2:latest",
		// Match Xvfb 1280x720; radar north-up, zoomed out; not rotating with view.
		LaunchOpts: "-novid -nojoy -low -vrdisable -console -w 1280 -h 720 +fps_max 30 +cl_disablehtmlmotd 1 -nosound -nopreload +r_dynamic 0 +mat_queue_mode 0 +con_enable 1 -condebug +r_player_visibility_mode 1 " +
			"+cl_radar_scale 0.30 +cl_radar_rotate 0 +cl_radar_always_centered 0 +cl_hud_radar_scale 1.15 " +
			"+cl_hud_radar_map_additive 0 +cl_hud_radar_blur_background 1 +cl_hud_radar_background_alpha 1",
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
