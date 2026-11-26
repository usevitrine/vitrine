// The screen and pointer checks scripts make (the CSS has the same breakpoints in its @media blocks). Reduced motion
// is lib/motion's reduced().
const matches = (query: string) => typeof matchMedia === 'function' && matchMedia(query).matches;

/** A phone's width: menus and dialogs come up as sheets from the bottom. */
export const PHONE = '(max-width: 639px)';
export const phone = () => matches(PHONE);

/** Wide enough for a column beside the page (the viewer's and the comic reader's details; narrower: a sheet). */
export const WIDE = '(min-width: 900px)';
export const wide = () => matches(WIDE);

/** A touch screen (a finger, no fine pointer). */
export const touch = () => matches('(pointer: coarse)');

/** A mouse (or trackpad) that can hover. */
export const fineMouse = () => matches('(hover: hover) and (pointer: fine)');
