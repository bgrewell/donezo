/** A permissive client-side email sanity check. It exists only to disable a
 *  submit button on obvious nonsense; the server does the real validation, so
 *  this deliberately stays loose (one @, a dot in the domain, no spaces). */
export function looksLikeEmail(value: string): boolean {
  return /^\S+@\S+\.\S+$/.test(value.trim());
}
