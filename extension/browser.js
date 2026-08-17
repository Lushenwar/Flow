/**
 * The one API difference worth abstracting between Chrome, Edge and Firefox.
 *
 * Edge is Chromium and needs nothing. Firefox implements the same MV3 surface
 * under `browser` with promises, and Chrome has provided promise-returning
 * `chrome.*` since MV3, so a single alias covers everything the extension uses.
 *
 * Deliberately not a polyfill library: this is four lines, and a dependency in
 * an extension is a supply-chain surface for a blocker that people install to
 * protect themselves.
 */
export const api =
  typeof browser !== "undefined" && browser.runtime ? browser : chrome;

/**
 * Firefox has no chrome.storage.session in older builds. Falling back to a
 * module-level object is correct rather than merely convenient: the cache only
 * exists to survive a service-worker suspend, and Firefox's event pages do not
 * suspend the same way.
 */
export const sessionStore = api.storage?.session ?? {
  _v: {},
  async get(key) {
    return { [key]: this._v[key] };
  },
  async set(obj) {
    Object.assign(this._v, obj);
  },
};
