export function ulid() {
  const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
  let time = Date.now();
  let prefix = "";
  for (let i = 0; i < 10; i++) {
    prefix = alphabet[time % 32] + prefix;
    time = Math.floor(time / 32);
  }
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  return prefix + Array.from(bytes, (byte) => alphabet[byte % 32]).join("");
}
