/**
 * jsdom does not implement <dialog>'s modal behaviour — showModal and close are
 * simply absent — so the components under test would throw before rendering.
 *
 * Stubbed to the minimum the tests actually observe: open/closed state and the
 * `close` event. The real focus trap is the browser's job and is not something a
 * DOM shim could verify anyway; what these tests check is that the markup asks
 * for it.
 */
if (typeof HTMLDialogElement !== "undefined") {
  if (!HTMLDialogElement.prototype.showModal) {
    HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
      this.open = true;
    };
  }
  if (!HTMLDialogElement.prototype.close) {
    HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
      this.open = false;
      this.dispatchEvent(new Event("close"));
    };
  }
}
