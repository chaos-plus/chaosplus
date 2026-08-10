import { isErrorEvent, isString } from "../../utils/index.mjs";
class ErrorEventCoercer {
    constructor(){}
    match(err) {
        if (!isErrorEvent(err)) return false;
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
        return isString(err.message) && err.message.length > 0;
    }
    _buildLocationStack(err) {
        const location = err;
        if (isString(location.filename) && location.filename.length > 0) {
            const lineno = location.lineno ?? 0;
            const colno = location.colno ?? 0;
            return `Error\n    at ${location.filename}:${lineno}:${colno}`;
        }
    }
}
export { ErrorEventCoercer };
