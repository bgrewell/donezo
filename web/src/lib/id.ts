/** Short unique id with a readable prefix, e.g. "act-3f9a12c4". */
export function newId(prefix: string): string {
  return `${prefix}-${crypto.randomUUID().slice(0, 8)}`;
}
