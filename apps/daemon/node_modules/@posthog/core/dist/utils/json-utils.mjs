const MAX_JSON_SAFE_VALUE_DEPTH = 20;
const MAX_JSON_SAFE_VALUE_ITEMS = 1000;
const MAX_JSON_SAFE_VALUE_NODES = 10000;
const CIRCULAR_VALUE = '[Circular]';
const TRUNCATED_VALUE = '[Truncated]';
const UNSERIALIZABLE_VALUE = '[Unserializable]';
const FUNCTION_VALUE = '[Function]';
const dateGetTime = Date.prototype.getTime;
const dateToISOString = Date.prototype.toISOString;
const propertyIsEnumerable = Object.prototype.propertyIsEnumerable;
function sanitizeString(value) {
    let output = '';
    for(let index = 0; index < value.length; index++){
        const codeUnit = value.charCodeAt(index);
        if (codeUnit >= 0xd800 && codeUnit <= 0xdbff) {
            const nextCodeUnit = value.charCodeAt(index + 1);
            if (nextCodeUnit >= 0xdc00 && nextCodeUnit <= 0xdfff) {
                output += value[index] + value[index + 1];
                index++;
            } else output += '\ufffd';
        } else output += codeUnit >= 0xdc00 && codeUnit <= 0xdfff ? '\ufffd' : value[index];
    }
    return output;
}
function toJsonSafeValue(value) {
    const state = {
        ancestors: new WeakSet(),
        remainingNodes: MAX_JSON_SAFE_VALUE_NODES
    };
    const convert = (current, depth)=>{
        if (state.remainingNodes <= 0) return TRUNCATED_VALUE;
        state.remainingNodes--;
        try {
            if (null == current || 'boolean' == typeof current) return current;
            if ('string' == typeof current) return sanitizeString(current);
            if ('number' == typeof current) return Number.isFinite(current) ? current : null;
            if ('bigint' == typeof current) return current.toString();
            if ('function' == typeof current) return FUNCTION_VALUE;
            if ('symbol' == typeof current) return current.description ? `Symbol(${current.description})` : 'Symbol()';
            if (depth >= MAX_JSON_SAFE_VALUE_DEPTH) return TRUNCATED_VALUE;
            if (state.ancestors.has(current)) return CIRCULAR_VALUE;
            state.ancestors.add(current);
            try {
                if (current instanceof Date) return Number.isFinite(dateGetTime.call(current)) ? dateToISOString.call(current) : null;
                let hasToJSONResult = false;
                let toJSONResult;
                try {
                    const toJSON = current.toJSON;
                    if ('function' == typeof toJSON) {
                        toJSONResult = toJSON.call(current);
                        hasToJSONResult = true;
                    }
                } catch  {
                    hasToJSONResult = false;
                }
                if (hasToJSONResult) return convert(toJSONResult, depth + 1);
                if (Array.isArray(current)) {
                    const itemCount = Math.min(current.length, MAX_JSON_SAFE_VALUE_ITEMS);
                    const output = [];
                    let index = 0;
                    for(; index < itemCount && state.remainingNodes > 0; index++)output.push(convert(current[index], depth + 1));
                    if (current.length > index) output.push(TRUNCATED_VALUE);
                    return output;
                }
                const output = {};
                let itemCount = 0;
                let truncated = false;
                for(const key in current){
                    if (!propertyIsEnumerable.call(current, key)) break;
                    if (itemCount >= MAX_JSON_SAFE_VALUE_ITEMS || state.remainingNodes <= 0) {
                        truncated = true;
                        break;
                    }
                    const converted = convert(current[key], depth + 1);
                    Object.defineProperty(output, key, {
                        value: converted,
                        enumerable: true,
                        configurable: true,
                        writable: true
                    });
                    itemCount++;
                }
                if (truncated) output[TRUNCATED_VALUE] = 'Additional properties omitted';
                return output;
            } finally{
                state.ancestors.delete(current);
            }
        } catch  {
            return UNSERIALIZABLE_VALUE;
        }
    };
    return convert(value, 0);
}
export { toJsonSafeValue };
