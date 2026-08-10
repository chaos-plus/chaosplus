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
    ErrorEventCoercer: ()=>ErrorEventCoercer
});
const index_js_namespaceObject = require("../../utils/index.js");
class ErrorEventCoercer {
    constructor(){}
    match(err) {
        if (!(0, index_js_namespaceObject.isErrorEvent)(err)) return false;
        const errorEvent = err;
        return void 0 != errorEvent.error || this._hasUsableMessage(errorEvent);
    }
    coerce(err, ctx) {
        if (void 0 != err.error) return ctx.apply(err.error);
        const exceptionLike = ctx.apply(err.message);
        return {
            ...exceptionLike,
            stack: this._buildLocationStack(err) ?? exceptionLike.stack,
            synthetic: true
        };
    }
    _hasUsableMessage(err) {
        return (0, index_js_namespaceObject.isString)(err.message) && err.message.length > 0;
    }
    _buildLocationStack(err) {
        const location = err;
        if ((0, index_js_namespaceObject.isString)(location.filename) && location.filename.length > 0) {
            const lineno = location.lineno ?? 0;
            const colno = location.colno ?? 0;
            return `Error\n    at ${location.filename}:${lineno}:${colno}`;
        }
    }
}
exports.ErrorEventCoercer = __webpack_exports__.ErrorEventCoercer;
for(var __webpack_i__ in __webpack_exports__)if (-1 === [
    "ErrorEventCoercer"
].indexOf(__webpack_i__)) exports[__webpack_i__] = __webpack_exports__[__webpack_i__];
Object.defineProperty(exports, '__esModule', {
    value: true
});
