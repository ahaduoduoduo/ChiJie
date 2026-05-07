// Lightweight inline icon set — stroke 1.6, 16x16 viewbox where possible.
const Ic = {};

const mk = (path, vb = "0 0 16 16") => (props = {}) => (
  <svg viewBox={vb} fill="none" stroke="currentColor" strokeWidth="1.5"
       strokeLinecap="round" strokeLinejoin="round" {...props}>
    {path}
  </svg>
);

Ic.dashboard = mk(<><rect x="2" y="2" width="5.5" height="5.5" rx="1"/><rect x="8.5" y="2" width="5.5" height="5.5" rx="1"/><rect x="2" y="8.5" width="5.5" height="5.5" rx="1"/><rect x="8.5" y="8.5" width="5.5" height="5.5" rx="1"/></>);
Ic.globe = mk(<><circle cx="8" cy="8" r="6"/><path d="M2 8h12M8 2c2 2 2 10 0 12M8 2c-2 2-2 10 0 12"/></>);
Ic.rss = mk(<><path d="M3 3a10 10 0 0 1 10 10M3 8a5 5 0 0 1 5 5"/><circle cx="3.5" cy="12.5" r="1"/></>);
Ic.layers = mk(<><path d="M8 2 2 5l6 3 6-3-6-3zM2 8l6 3 6-3M2 11l6 3 6-3"/></>);
Ic.shield = mk(<><path d="M8 2 3 4v4c0 3 2 5 5 6 3-1 5-3 5-6V4L8 2z"/><path d="m6 8 1.5 1.5L10 7"/></>);
Ic.activity = mk(<><path d="M2 8h3l2-5 2 10 2-5h3"/></>);
Ic.cog = mk(<><circle cx="8" cy="8" r="2.2"/><path d="M8 1.5v1.5M8 13v1.5M14.5 8H13M3 8H1.5M12.6 3.4l-1.1 1.1M4.5 11.5l-1.1 1.1M12.6 12.6l-1.1-1.1M4.5 4.5 3.4 3.4"/></>);
Ic.plus = mk(<><path d="M8 3v10M3 8h10"/></>);
Ic.refresh = mk(<><path d="M13.5 8a5.5 5.5 0 1 1-1.6-3.9"/><path d="M14 2.5V5h-2.5"/></>);
Ic.search = mk(<><circle cx="7" cy="7" r="4.5"/><path d="m13.5 13.5-3-3"/></>);
Ic.dots = mk(<><circle cx="3" cy="8" r="1"/><circle cx="8" cy="8" r="1"/><circle cx="13" cy="8" r="1"/></>);
Ic.edit = mk(<><path d="M11 2.5 13.5 5 5 13.5H2.5V11L11 2.5z"/></>);
Ic.trash = mk(<><path d="M2.5 4h11M5 4V2.5h6V4M4 4l.5 9.5h7L12 4M6.5 6.5v5M9.5 6.5v5"/></>);
Ic.power = mk(<><path d="M5.5 4.5a5 5 0 1 0 5 0M8 1.5v6"/></>);
Ic.test = mk(<><path d="M6 1.5h4M6.5 1.5v4L3 12.5a1.5 1.5 0 0 0 1.4 2h7.2a1.5 1.5 0 0 0 1.4-2L9.5 5.5v-4"/></>);
Ic.copy = mk(<><rect x="5" y="5" width="9" height="9" rx="1"/><path d="M11 5V3a1 1 0 0 0-1-1H3a1 1 0 0 0-1 1v7a1 1 0 0 0 1 1h2"/></>);
Ic.eye = mk(<><path d="M1.5 8S4 3.5 8 3.5 14.5 8 14.5 8 12 12.5 8 12.5 1.5 8 1.5 8z"/><circle cx="8" cy="8" r="2"/></>);
Ic.x = mk(<><path d="m3.5 3.5 9 9M12.5 3.5l-9 9"/></>);
Ic.check = mk(<><path d="m3 8 3.5 3.5L13 5"/></>);
Ic.chev = mk(<><path d="m4 6 4 4 4-4"/></>);
Ic.ext = mk(<><path d="M9 2.5h4.5V7M13.5 2.5 7.5 8.5M11 9v3.5H3.5V5H7"/></>);
Ic.bolt = mk(<><path d="M9 1.5 3.5 9h4l-1 5.5L13 7H9l1-5.5z"/></>);
Ic.alert = mk(<><path d="M8 2 1.5 13.5h13L8 2zM8 6.5v3M8 11.5v.5"/></>);
Ic.sub = mk(<><rect x="2.5" y="3" width="11" height="10" rx="1"/><path d="M2.5 6h11M5 9h6"/></>);
Ic.tpl = mk(<><rect x="2.5" y="2.5" width="11" height="11" rx="1"/><path d="M5.5 2.5v11M2.5 5.5h11"/></>);
Ic.lock = mk(<><rect x="3" y="7" width="10" height="7" rx="1"/><path d="M5 7V5a3 3 0 0 1 6 0v2"/></>);
Ic.filter = mk(<><path d="M2 3h12l-4.5 5.5V13L6.5 11.5V8.5L2 3z"/></>);
Ic.download = mk(<><path d="M8 2v8m-3-3 3 3 3-3M2.5 13h11"/></>);
Ic.upload = mk(<><path d="M8 13V5m-3 3 3-3 3 3M2.5 2.5h11"/></>);
Ic.term = mk(<><rect x="1.5" y="2.5" width="13" height="11" rx="1"/><path d="m4 6 2.5 2L4 10M8 10h4"/></>);

window.Ic = Ic;
