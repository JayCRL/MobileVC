/**
 * MobileVC mobile-surface contract (initial draft).
 *
 * 事件的权威来源是 apps/dsh/plugin/index.mjs；本文件是它的类型化投影。
 * 只增不改：加字段可以，改语义不行。接收方遇到未知 type 必须忽略而不是抛错。
 */

/** 会话状态 */
export type SessionStatus = 'idle' | 'running' | 'waiting-approval' | 'error';

/** 发送模式：排队 or 立即插话 */
export type PromptMode = 'queue' | 'steer';

/** 审批决定 */
export type ApprovalDecision = 'allow-once' | 'always' | 'deny';

export interface SessionRef {
  sessionId: string;
  title: string;
  status: SessionStatus;
  updatedAt: string;
  /** 是否仍持有可用流 */
  live?: boolean;
}

export interface ToolCall {
  callId: string;
  name: string;
  /** 参数已序列化为字符串，长度由实现截断 */
  args?: string;
  status: 'pending' | 'ok' | 'error';
}

export interface DiffFile {
  path: string;
  status: 'added' | 'modified' | 'deleted';
  /** 统一 diff 文本；分组与回滚语义由客户端实现 */
  hunks: string;
}

export interface ApprovalRequest {
  callId: string;
  /** 来源工具或审批类别 */
  kind: string;
  title: string;
  detail?: string;
  /** 可选动作；缺省时客户端给出 allow-once / deny */
  actions?: ApprovalDecision[];
  createdAt: string;
}

/** 服务端 → 客户端 */
export type SurfaceEvent =
  | { type: 'snapshot'; session: SessionRef; messages: unknown[] }
  | { type: 'session/title'; sessionId: string; title: string }
  | { type: 'session/end-seed'; sessionId: string }
  | { type: 'turn/start'; sessionId: string; turnId: string }
  | { type: 'turn/end'; sessionId: string; turnId: string; reason?: string }
  | { type: 'assistant-stream'; sessionId: string; turnId: string; text: string; kind: 'text' | 'reasoning' }
  | { type: 'assistant/message'; sessionId: string; turnId: string; text: string }
  | { type: 'tool/call'; sessionId: string; call: ToolCall }
  | { type: 'tool/result'; sessionId: string; call: ToolCall; output?: string }
  | { type: 'approval/asked'; sessionId: string; request: ApprovalRequest }
  | { type: 'approval/decided'; sessionId: string; callId: string; decision: ApprovalDecision }
  | { type: 'compaction/summary'; sessionId: string; summary: string }
  | { type: 'request/header'; sessionId: string; header: Record<string, unknown> };

/** 客户端 → 服务端 */
export type SurfaceCommand =
  | { type: 'prompt'; sessionId: string; text: string; mode: PromptMode; attachments?: string[] }
  | { type: 'cancel'; sessionId: string }
  | { type: 'approval/decide'; sessionId: string; callId: string; decision: ApprovalDecision };
