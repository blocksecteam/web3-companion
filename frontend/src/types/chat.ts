// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

export type Page = "landing" | "auth" | "chat" | "portfolio" | "transactions" | "policies" | "audit" | "wallets" | "settings" | "confirm";

export type AuthPhase =
  | "email"
  | "login-passkey"
  | "register-passkey"
  | "confirm-passkey"
  | "complete";

export type User = {
  userId: string;
  email: string;
  accessToken?: string;
  refreshToken?: string;
  tokenType?: string;
  expiresAt?: number;
  authSessionId?: string;
  credentialId?: string;
};

export type PasskeyApprovalRequest = {
  challengeId: string;
  signUrl: string;
};

export type SessionSummary = {
  id: string;
  title: string;
  createdAt: string;
  updatedAt: string;
  status: "idle" | "running";
  messageCount: number;
};

export type ToolCallKind =
  | "portfolio"
  | "swap"
  | "bridge"
  | "security"
  | "wallet"
  | "chain"
  | "token"
  | "web3"
  | "skill"
  | "command"
  | "file"
  | "search"
  | "tool";

export type ChatRole = "assistant" | "user" | "system";

export type ToolCallView = {
  id: string;
  name: string;
  kind: ToolCallKind;
  status: "queued" | "running" | "success" | "error";
  input?: unknown;
};

export type ChatMessage = {
  id: string;
  role: ChatRole;
  content: string;
  createdAt: string;
  thinking?: string;
  partial?: boolean;
  tools?: ToolCallView[];
  blocks?: ChatBlock[];
};

export type SecurityReviewBlock = {
  id: string;
  type: "security_review";
  status: "reviewing" | "safe" | "unsafe" | "error";
  transactionId?: string;
  markdown: string;
};

export type PasskeyRequestBlock = {
  id: string;
  type: "passkey_request";
  status: "pending";
  challengeId: string;
  signUrl: string;
  operation: "sign" | "delegate" | string;
  transactionId?: string;
  summary?: Record<string, unknown>;
};

export type ChatBlock = SecurityReviewBlock | PasskeyRequestBlock;

export type ClientWsEvent =
  | {
      type: "user_message";
      id: string;
      sessionId: string;
      content: string;
    }
  | {
      type: "stop";
      sessionId: string;
    };

export type ServerWsEvent =
  | {
      type: "session";
      session: SessionSummary;
    }
  | {
      type: "message";
      sessionId: string;
      message: ChatMessage;
    }
  | {
      type: "partial";
      id: string;
      sessionId: string;
      delta: string;
      kind?: "text" | "thinking";
    }
  | {
      type: "tool";
      messageId: string;
      sessionId: string;
      tool: ToolCallView;
    }
  | {
      type: "block_update";
      sessionId: string;
      messageId: string;
      block: ChatBlock;
    }
  | {
      type: "status";
      sessionId: string;
      status: "idle" | "running";
    }
  | {
      type: "error";
      sessionId?: string;
      message: string;
    };
