package autoplay

// Распределение полей cs2_memory.json (глобалы libclient + оффсеты полей пешки) по задачам фермы.
// Чтение памяти: linuxCS2Mem в cs2mem_linux.go — snapshot(), espDets(), resolveLocalPawn().

// MemReadTask — подсистема, которая использует конфиг для process_vm_readv.
type MemReadTask int

const (
	// MemTaskModulePick — выбор сегмента libclient в /proc/<pid>/maps (не игровые данные).
	MemTaskModulePick MemReadTask = iota

	// MemTaskPawnPose — мир поза локального игрока: XYZ, pitch/yaw, скорость.
	// Потребители: pollCS2MemoryNav → snapshot → ingestMemWorldPositionLocked, emitMemTelemetry, mapnav.
	// Код: resolveLocalPawn, snapshot.
	MemTaskPawnPose

	// MemTaskEspOverlay — враги на экране без YOLO (матрица + список сущностей + поля пешки).
	// Потребители: tickMemEspOverlay → espDets.
	// Код: readViewMatrix, entityPawnAtIndex, цикл по индексам (опционально GES для maxI).
	MemTaskEspOverlay

	// MemTaskSigscanFill — только глобальные dw_* из .text, если в конфиге нули (sigscan).
	// Потребители: tryStartLinuxMemDriver + applySigScanToOffsets.
	MemTaskSigscanFill

	// MemTaskDebugHTTP — /rva-probe, hex оффсетов в JSON снимке (cs2mem_debug_http_linux).
	MemTaskDebugHTTP
)

// JSONKeysForTask возвращает ключи из cs2_memory.json, относящиеся к задаче (для доков и проверки конфига).
func JSONKeysForTask(t MemReadTask) []string {
	switch t {
	case MemTaskModulePick:
		return []string{"module_substr", "module_path_contains"}
	case MemTaskPawnPose:
		return []string{
			"dw_local_player_pawn",
			"dw_entity_list",
			"dw_local_player_controller",
			"m_h_player_pawn",
			"m_v_old_origin",
			"m_ang_eye_angles",
			"m_vec_abs_velocity",
		}
	case MemTaskEspOverlay:
		return []string{
			"dw_view_matrix",
			"dw_entity_list",
			"dw_local_player_pawn",
			"dw_local_player_controller",
			"m_h_player_pawn",
			"m_v_old_origin",
			"m_ang_eye_angles",
			"dw_game_entity_system",
			"dw_game_entity_system_highest_index",
			"m_i_team_num",
			"m_i_health",
			"m_life_state",
			"entity_list_stride",
			"esp_player_height",
		}
	case MemTaskSigscanFill:
		return []string{
			"dw_local_player_pawn",
			"dw_entity_list",
			"dw_local_player_controller",
			"dw_view_matrix",
			"dw_game_entity_system",
			"dw_game_entity_system_highest_index",
		}
	case MemTaskDebugHTTP:
		return []string{
			"data_section_start_rva",
			"data_section_plus_20h_rva",
			"esp_eye_z_offset",
			"dw_local_player_pawn",
			"dw_entity_list",
			"dw_view_matrix",
		}
	default:
		return nil
	}
}

// MemReadTaskSummary — одна строка на задачу: что читается и зачем.
func MemReadTaskSummary(t MemReadTask) string {
	switch t {
	case MemTaskModulePick:
		return "поиск базы libclient.so в памяти процесса CS2"
	case MemTaskPawnPose:
		return "позиция/углы/скорость пешки → навигация, телеметрия WebSocket, сравнение с GSI"
	case MemTaskEspOverlay:
		return "view matrix + entity list + поля C_BaseEntity → 2D-боксы врагов для оверлея"
	case MemTaskSigscanFill:
		return "заполнение нулевых dw_* по сигнатурам .text (libclient)"
	case MemTaskDebugHTTP:
		return "HTTP /rva-probe: проба qword по so/rva_table.json"
	default:
		return ""
	}
}

// AllMemReadTasks порядок для справки UI/логов.
func AllMemReadTasks() []MemReadTask {
	return []MemReadTask{
		MemTaskModulePick,
		MemTaskSigscanFill,
		MemTaskPawnPose,
		MemTaskEspOverlay,
		MemTaskDebugHTTP,
	}
}
