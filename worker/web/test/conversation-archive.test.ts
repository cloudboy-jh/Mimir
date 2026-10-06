import { afterEach, describe, expect, it, vi } from "vitest";
import { getConversationExchange } from "../src/lib/api";

const headers = { "content-type": "application/json" };
afterEach(() => vi.unstubAllGlobals());

describe("bounded conversation archives", () => {
  it("cancels an oversized stream without a declared content length", async () => {
    let cancelled = false;
    let calls = 0;
    const archive = new ReadableStream<Uint8Array>({
      pull(controller) { controller.enqueue(new Uint8Array(1024 * 1024)); },
      cancel() { cancelled = true; },
    });
    vi.stubGlobal("fetch", vi.fn(async () => calls++ === 0
      ? new Response(JSON.stringify({ exchange: { id: "large" }, log_url: "/dashboard/log-objects/log/large.json" }), { headers })
      : new Response(archive, { headers })));
    await expect(getConversationExchange("large")).rejects.toMatchObject({ status: 413 });
    expect(cancelled).toBe(true);
  });

  it("decodes multibyte text split across streaming chunks", async () => {
    const text = JSON.stringify({ response: { format: "json", body: { content: "résumé ✓" } } });
    const bytes = new TextEncoder().encode(text);
    const split = bytes.indexOf(0xc3) + 1;
    let calls = 0;
    const archive = new ReadableStream<Uint8Array>({ start(controller) {
      controller.enqueue(bytes.slice(0, split));
      controller.enqueue(bytes.slice(split));
      controller.close();
    } });
    vi.stubGlobal("fetch", vi.fn(async () => calls++ === 0
      ? new Response(JSON.stringify({ exchange: { id: "unicode" }, log_url: "/dashboard/log-objects/log/unicode.json" }), { headers })
      : new Response(archive, { headers })));
    const result = await getConversationExchange("unicode");
    expect(result.envelope.response).toEqual({ format: "json", body: { content: "résumé ✓" } });
  });
});
