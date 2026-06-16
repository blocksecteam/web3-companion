// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { Database } from "bun:sqlite";
import type { SDKMessage } from "@anthropic-ai/claude-agent-sdk";
import type { ChatMessage, SessionSummary } from "../types/chat";
import { sdkMessageType, sdkMessageUUID, sdkParentToolUseID } from "./sdk-archive";

type SessionRow = {
  id: string;
  title: string;
  created_at: string;
  updated_at: string;
  status: "idle" | "running";
  message_count: number;
  has_claude_session: number;
};

type MessageRow = {
  sequence: number;
  session_id: string;
  message_id: string;
  role: ChatMessage["role"];
  content: string;
  thinking: string | null;
  created_at: string;
  partial: number;
  tools: string | null;
  blocks: string | null;
};

type RawSDKMessageRow = {
  sequence: number;
  session_id: string;
  sdk_uuid: string | null;
  sdk_type: string;
  parent_tool_use_id: string | null;
  created_at: string;
  raw: string;
};

export type RawSDKMessageEntry = {
  sequence: number;
  sessionId: string;
  createdAt: string;
  message: SDKMessage;
};

export class SessionStore {
  private readonly db: Database;

  constructor(
    dbPath = resolve(
      process.env.SESSION_DB_PATH ?? `dist/companion-${process.env.COMPANION_PORT ?? 8787}.sqlite`,
    ),
  ) {
    mkdirSync(dirname(dbPath), { recursive: true });
    this.db = new Database(dbPath);
    this.db.run("PRAGMA journal_mode = WAL");
    this.db.run("PRAGMA foreign_keys = ON");
    this.migrate();
  }

  listSessions(): SessionSummary[] {
    const rows = this.db
      .query<SessionRow, []>(
        `select id, title, created_at, updated_at, status, message_count, has_claude_session
         from sessions
         where message_count > 0
         order by updated_at desc`,
      )
      .all();

    return rows.map(sessionFromRow);
  }

  getSession(id: string) {
    const row = this.db
      .query<SessionRow, [string]>(
        `select id, title, created_at, updated_at, status, message_count, has_claude_session
         from sessions
         where id = ?`,
      )
      .get(id);
    return row ? { ...sessionFromRow(row), hasClaudeSession: row.has_claude_session === 1 } : null;
  }

  ensureSession(id: string, title = "New on-chain conversation") {
    const now = new Date().toISOString();
    this.db
      .query(
        `insert into sessions (id, title, created_at, updated_at, status, message_count, has_claude_session)
         values (?, ?, ?, ?, 'idle', 0, 0)
         on conflict(id) do nothing`,
      )
      .run(id, title, now, now);
    return this.getSession(id);
  }

  updateSessionStatus(id: string, status: "idle" | "running") {
    this.ensureSession(id);
    this.db
      .query("update sessions set status = ?, updated_at = ? where id = ?")
      .run(status, new Date().toISOString(), id);
    return this.getSession(id);
  }

  markClaudeSession(id: string) {
    this.db.query("update sessions set has_claude_session = 1 where id = ?").run(id);
  }

  renameFromFirstMessage(id: string, content: string) {
    const title = content.trim().replace(/\s+/g, " ").slice(0, 64) || "New on-chain conversation";
    this.db.query("update sessions set title = ? where id = ? and title = ?").run(
      title,
      id,
      "New on-chain conversation",
    );
  }

  addMessage(sessionId: string, message: ChatMessage): ChatMessage {
    this.ensureSession(sessionId);
    const tools = message.tools ? JSON.stringify(message.tools) : null;
    const blocks = message.blocks ? JSON.stringify(message.blocks) : null;
    const partial = message.partial ? 1 : 0;

    const existing = this.db
      .query<{ sequence: number }, [string, string]>(
        "select sequence from messages where session_id = ? and message_id = ?",
      )
      .get(sessionId, message.id);

    if (existing) {
      this.db
        .query(
          `update messages
           set role = ?, content = ?, thinking = ?, created_at = ?, partial = ?, tools = ?, blocks = ?
           where session_id = ? and message_id = ?`,
        )
        .run(
          message.role,
          message.content,
          message.thinking ?? null,
          message.createdAt,
          partial,
          tools,
          blocks,
          sessionId,
          message.id,
        );
      this.refreshSessionCount(sessionId);
      return this.getMessage(existing.sequence);
    }

    const result = this.db
      .query(
        `insert into messages (session_id, message_id, role, content, thinking, created_at, partial, tools, blocks)
         values (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
      )
      .run(
        sessionId,
        message.id,
        message.role,
        message.content,
        message.thinking ?? null,
        message.createdAt,
        partial,
        tools,
        blocks,
      );
    this.refreshSessionCount(sessionId);
    return this.getMessage(Number(result.lastInsertRowid));
  }

  listMessages(sessionId: string): ChatMessage[] {
    return this.db
      .query<MessageRow, [string]>(
        `select sequence, session_id, message_id, role, content, thinking, created_at, partial, tools, blocks
         from messages
         where session_id = ?
         order by sequence asc`,
      )
      .all(sessionId)
      .map(messageFromRow);
  }

  addRawSDKMessage(
    sessionId: string,
    message: SDKMessage,
    createdAt = new Date().toISOString(),
  ): RawSDKMessageEntry {
    this.ensureSession(sessionId);
    const raw = JSON.stringify(message);
    const sdkUUID = sdkMessageUUID(message) || null;
    const sdkType = sdkMessageType(message);
    const parentToolUseID = sdkParentToolUseID(message);

    if (sdkUUID) {
      const existing = this.db
        .query<{ sequence: number }, [string, string]>(
          "select sequence from raw_sdk_messages where session_id = ? and sdk_uuid = ?",
        )
        .get(sessionId, sdkUUID);
      if (existing) {
        this.db
          .query(
            `update raw_sdk_messages
             set sdk_type = ?, parent_tool_use_id = ?, created_at = ?, raw = ?
             where session_id = ? and sdk_uuid = ?`,
          )
          .run(sdkType, parentToolUseID, createdAt, raw, sessionId, sdkUUID);
        return this.getRawSDKMessage(existing.sequence);
      }
    }

    const result = this.db
      .query(
        `insert into raw_sdk_messages (session_id, sdk_uuid, sdk_type, parent_tool_use_id, created_at, raw)
         values (?, ?, ?, ?, ?, ?)`,
      )
      .run(sessionId, sdkUUID, sdkType, parentToolUseID, createdAt, raw);
    return this.getRawSDKMessage(Number(result.lastInsertRowid));
  }

  listRawSDKMessages(sessionId: string): RawSDKMessageEntry[] {
    return this.db
      .query<RawSDKMessageRow, [string]>(
        `select sequence, session_id, sdk_uuid, sdk_type, parent_tool_use_id, created_at, raw
         from raw_sdk_messages
         where session_id = ?
         order by sequence asc`,
      )
      .all(sessionId)
      .map(rawSDKMessageEntryFromRow);
  }

  deleteSession(id: string) {
    this.db.query("delete from sessions where id = ?").run(id);
  }

  private migrate() {
    this.db.run(`
      create table if not exists sessions (
        id text primary key,
        title text not null,
        created_at text not null,
        updated_at text not null,
        status text not null default 'idle',
        message_count integer not null default 0,
        has_claude_session integer not null default 0
      )
    `);

    this.db.run(`
      create table if not exists messages (
        sequence integer primary key autoincrement,
        session_id text not null references sessions(id) on delete cascade,
        message_id text not null,
        role text not null,
        content text not null,
        thinking text,
        created_at text not null,
        partial integer not null default 0,
        tools text,
        blocks text
      )
    `);
    this.addColumnIfMissing("messages", "blocks", "text");

    this.db.run(`
      create unique index if not exists idx_messages_session_message
      on messages(session_id, message_id)
    `);
    this.db.run(
      "create index if not exists idx_messages_session_sequence on messages(session_id, sequence)",
    );

    this.db.run(`
      create table if not exists raw_sdk_messages (
        sequence integer primary key autoincrement,
        session_id text not null references sessions(id) on delete cascade,
        sdk_uuid text,
        sdk_type text not null,
        parent_tool_use_id text,
        created_at text not null,
        raw text not null
      )
    `);

    this.db.run(`
      create unique index if not exists idx_raw_sdk_messages_session_uuid
      on raw_sdk_messages(session_id, sdk_uuid)
      where sdk_uuid is not null
    `);
    this.db.run(
      "create index if not exists idx_raw_sdk_messages_session_sequence on raw_sdk_messages(session_id, sequence)",
    );
  }

  private getMessage(sequence: number): ChatMessage {
    const row = this.db
      .query<MessageRow, [number]>(
        `select sequence, session_id, message_id, role, content, thinking, created_at, partial, tools, blocks
         from messages
         where sequence = ?`,
      )
      .get(sequence);
    if (!row) throw new Error(`Message not found: ${sequence}`);
    return messageFromRow(row);
  }

  private getRawSDKMessage(sequence: number): RawSDKMessageEntry {
    const row = this.db
      .query<RawSDKMessageRow, [number]>(
        `select sequence, session_id, sdk_uuid, sdk_type, parent_tool_use_id, created_at, raw
         from raw_sdk_messages
         where sequence = ?`,
      )
      .get(sequence);
    if (!row) throw new Error(`Raw SDK message not found: ${sequence}`);
    return rawSDKMessageEntryFromRow(row);
  }

  private refreshSessionCount(sessionId: string) {
    const count = this.db
      .query<{ count: number }, [string]>("select count(*) as count from messages where session_id = ?")
      .get(sessionId)?.count;
    this.db
      .query("update sessions set updated_at = ?, message_count = ? where id = ?")
      .run(new Date().toISOString(), count ?? 0, sessionId);
  }

  private addColumnIfMissing(table: string, column: string, definition: string) {
    const rows = this.db.query<{ name: string }, []>(`pragma table_info(${table})`).all();
    if (rows.some((row) => row.name === column)) return;
    this.db.run(`alter table ${table} add column ${column} ${definition}`);
  }
}

export function createSessionStoreFromConfig(raw = process.env.COMPANION_SESSION_STORE) {
  if (!raw) return new SessionStore();

  const sqlitePrefix = "sqlite:file=";
  if (raw.startsWith(sqlitePrefix)) {
    const dbPath = raw.slice(sqlitePrefix.length).trim();
    if (!dbPath) {
      throw new Error("COMPANION_SESSION_STORE sqlite:file requires a database path.");
    }
    return new SessionStore(resolve(dbPath));
  }

  throw new Error(`Unsupported companion session store: ${raw}`);
}

function sessionFromRow(row: SessionRow): SessionSummary {
  return {
    id: row.id,
    title: row.title,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
    status: row.status,
    messageCount: row.message_count,
  };
}

function messageFromRow(row: MessageRow): ChatMessage {
  return {
    id: row.message_id,
    role: row.role,
    content: row.content,
    thinking: row.thinking ?? undefined,
    createdAt: row.created_at,
    partial: row.partial === 1,
    tools: row.tools ? JSON.parse(row.tools) : undefined,
    blocks: row.blocks ? JSON.parse(row.blocks) : undefined,
  };
}

function rawSDKMessageEntryFromRow(row: RawSDKMessageRow): RawSDKMessageEntry {
  return {
    sequence: row.sequence,
    sessionId: row.session_id,
    createdAt: row.created_at,
    message: JSON.parse(row.raw),
  };
}
