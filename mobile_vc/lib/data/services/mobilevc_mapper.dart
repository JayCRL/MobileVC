import '../models/events.dart';
import '../models/runtime_meta.dart';

class MobileVcMapper {
  const MobileVcMapper();

  AppEvent mapEvent(Map<String, dynamic> json) {
    final type = (json['type'] ?? '').toString();
    switch (type) {
      case 'log':
        return LogEvent.fromJson(json);
      case 'progress':
        return ProgressEvent.fromJson(json);
      case 'error':
        return ErrorEvent.fromJson(json);
      case 'prompt_request':
        return PromptRequestEvent.fromJson(json);
      case 'interaction_request':
        return InteractionRequestEvent.fromJson(json);
      case 'session_state':
        return SessionStateEvent.fromJson(json);
      case 'runtime_phase':
        return RuntimePhaseEvent.fromJson(json);
      case 'agent_state':
        return AgentStateEvent.fromJson(json);
      case 'fs_list_result':
        return FSListResultEvent.fromJson(json);
      case 'fs_read_result':
        return FSReadResultEvent.fromJson(json);
      case 'step_update':
        return StepUpdateEvent.fromJson(json);
      case 'file_diff':
        return FileDiffEvent.fromJson(json);
      case 'runtime_info_result':
        return RuntimeInfoResultEvent.fromJson(json);
      case 'runtime_process_list_result':
        return RuntimeProcessListResultEvent.fromJson(json);
      case 'runtime_process_log_result':
        return RuntimeProcessLogResultEvent.fromJson(json);
      case 'session_created':
        return SessionCreatedEvent.fromJson(json);
      case 'session_list_result':
        return SessionListResultEvent.fromJson(json);
      case 'session_history':
        return SessionHistoryEvent.fromJson(json);
      case 'review_state':
        return ReviewStateEvent.fromJson(json);
      case 'skill_catalog_result':
        return SkillCatalogResultEvent.fromJson(json);
      case 'memory_list_result':
        return MemoryListResultEvent.fromJson(json);
      case 'session_context_result':
        return SessionContextResultEvent.fromJson(json);
      case 'permission_rule_list_result':
        return PermissionRuleListResultEvent.fromJson(json);
      case 'permission_auto_applied':
        return PermissionAutoAppliedEvent.fromJson(json);
      case 'skill_sync_result':
        return SkillSyncResultEvent.fromJson(json);
      case 'catalog_sync_status':
        return CatalogSyncStatusEvent.fromJson(json);
      case 'catalog_sync_result':
        return CatalogSyncResultEvent.fromJson(json);
      case 'adb_devices_result':
        return AdbDevicesResultEvent.fromJson(json);
      case 'adb_stream_state':
        return AdbStreamStateEvent.fromJson(json);
      case 'adb_frame':
        return AdbFrameEvent.fromJson(json);
      case 'adb_webrtc_answer':
        return AdbWebRtcAnswerEvent.fromJson(json);
      case 'adb_webrtc_state':
        return AdbWebRtcStateEvent.fromJson(json);
      default:
        return UnknownEvent(
          type: type,
          timestamp: DateTime.now(),
          sessionId: (json['sessionId'] ?? '').toString(),
          runtimeMeta: const RuntimeMeta(),
          raw: json,
        );
    }
  }
}
