import type { Envelope } from "./transcript";
import { readBoundedText } from "./response-codec";

export const MAX_ARCHIVE_READ_BYTES = 2 * 1024 * 1024;
export const ARCHIVE_READ_CONCURRENCY = 2;

/** Reads a saved exchange envelope from R2; null when missing, oversized or malformed. */
export async function readArchive(bucket: R2Bucket, key: string): Promise<Envelope | null> {
  if (!key) return null;
  try {
    const object = await bucket.get(key);
    if (!object) return null;
    if (object.size > MAX_ARCHIVE_READ_BYTES) {
      await object.body.cancel();
      return null;
    }
    const envelope = JSON.parse(await readBoundedText(object.body, MAX_ARCHIVE_READ_BYTES)) as unknown;
    return envelope && typeof envelope === "object" && !Array.isArray(envelope) ? (envelope as Envelope) : null;
  } catch {
    return null;
  }
}

/** Reads archives with at most ARCHIVE_READ_CONCURRENCY R2 reads in flight, preserving order. */
export async function readArchives(bucket: R2Bucket, keys: string[]): Promise<Array<Envelope | null>> {
  const results: Array<Envelope | null> = new Array(keys.length).fill(null);
  let next = 0;
  const worker = async () => {
    while (next < keys.length) {
      const index = next++;
      results[index] = await readArchive(bucket, keys[index]!);
    }
  };
  await Promise.all(Array.from({ length: Math.min(ARCHIVE_READ_CONCURRENCY, keys.length) }, worker));
  return results;
}
