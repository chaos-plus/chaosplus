"use strict";
var __webpack_require__ = {};
(()=>{
    __webpack_require__.d = (exports1, definition)=>{
        for(var key in definition)if (__webpack_require__.o(definition, key) && !__webpack_require__.o(exports1, key)) Object.defineProperty(exports1, key, {
            enumerable: true,
            get: definition[key]
        });
    };
})();
(()=>{
    __webpack_require__.o = (obj, prop)=>Object.prototype.hasOwnProperty.call(obj, prop);
})();
(()=>{
    __webpack_require__.r = (exports1)=>{
        if ('undefined' != typeof Symbol && Symbol.toStringTag) Object.defineProperty(exports1, Symbol.toStringTag, {
            value: 'Module'
        });
        Object.defineProperty(exports1, '__esModule', {
            value: true
        });
    };
})();
var __webpack_exports__ = {};
__webpack_require__.r(__webpack_exports__);
__webpack_require__.d(__webpack_exports__, {
    isWebKit: ()=>isWebKit
});
const external_string_utils_js_namespaceObject = require("./string-utils.js");
function isWebKit(userAgent) {
    return (0, external_string_utils_js_namespaceObject.includes)(userAgent, 'AppleWebKit') && !(0, external_string_utils_js_namespaceObject.includes)(userAgent, 'Chrome');
}
exports.isWebKit = __webpack_exports__.isWebKit;
for(var __webpack_i__ in __webpack_exports__)if (-1 === [
    "isWebKit"
].indexOf(__webpack_i__)) exports[__webpack_i__] = __webpack_exports__[__webpack_i__];
Object.defineProperty(exports, '__esModule', {
    value: true
});
