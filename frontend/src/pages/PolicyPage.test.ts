// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const source = [
  readFileSync(new URL("./PolicyPage.tsx", import.meta.url), "utf8"),
  readFileSync(new URL("./policies/policy-model.ts", import.meta.url), "utf8"),
  readFileSync(new URL("./policies/policy-ui.tsx", import.meta.url), "utf8"),
].join("\n");

describe("PolicyPage input validation", () => {
  test("validates policy names and numeric policy fields before save", () => {
    expect(source).toContain("validatePolicyDraftFields");
    expect(source).toContain("validateNameInput(draft.name)");
    expect(source).toContain("validateDecimalInput");
    expect(source).toContain("validateIntegerInput");
    expect(source).toContain("hasFieldErrors");
    expect(source).toContain("aria-invalid={Boolean(error)}");
  });

  test("restores policy delete actions from stored display names", () => {
    expect(source).toContain('kind: "policy_definition_delete", policyDefinitionId: action.definitionId, name: action.name');
    expect(source).toContain('kind: "delete", definitionId: stored.policyDefinitionId, name: stored.name');
    expect(source).not.toContain("definition?.name ?? stored.policyDefinitionId");
  });
});
