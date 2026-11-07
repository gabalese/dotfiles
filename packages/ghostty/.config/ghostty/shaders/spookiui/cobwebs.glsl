#define SPOOKI_TIME (iTime - iTimeFocus)
const float SPOOKI_VIBRANCY = 1.00;
const float SPOOKI_DIM = 1.0;

// added by SpookiUI: the treat animation only ever plays in the focused window.
// `iFocus` is 1.0 while focused, 0.0 while unfocused. When this window is
// unfocused we hide the effect and either:
//   - dim + desaturate the terminal (SPOOKI_DIM baked in), so the inactive
//     window is set apart from the focused one; or
//   - render the plain, untouched terminal when dimming is off.
vec4 spooki_out(vec3 res, vec4 term) {
    if (iFocus < 0.5) {
        if (SPOOKI_DIM > 0.5) {
            float g = dot(term.rgb, vec3(0.2126, 0.7152, 0.0722));  // luminance
            vec3 inactive = mix(term.rgb, vec3(g), 0.55) * 0.55;    // desaturate, dim
            return vec4(inactive, term.a);
        }
        return term;                                               // plain, no effect
    }
    vec3 fx = term.rgb + (res - term.rgb) * SPOOKI_VIBRANCY;    // scale the effect
    return vec4(min(fx, vec3(1.0)), term.a);
}

// SpookiUI treat: Cobwebs.
// Faint spider webs strung across two corners, with a gentle shimmer — mostly
// still. Original SpookiUI shader; drawn over dark background pixels only.

float webAt(vec2 uv, vec2 corner, float aspect, float t) {
    vec2 p = uv - corner;
    p.x *= aspect;
    p = abs(p);
    float r = length(p);
    float maxR = 0.5;
    if (r > maxR) return 0.0;
    float a = atan(p.y, p.x);                            // 0..pi/2
    float spoke = abs(fract(a / 1.5708 * 6.0) - 0.5);    // 6 radial threads
    float spokeL = smoothstep(0.055, 0.0, spoke);
    float ring = abs(fract(r * 8.0) - 0.5);              // concentric threads
    float ringL = smoothstep(0.06, 0.0, ring) * step(0.04, r);
    float web = max(spokeL, ringL);
    float fade = smoothstep(maxR, 0.05, r);              // denser toward the corner
    float shimmer = 0.8 + 0.2 * sin(t * 1.2 + r * 22.0);
    return web * fade * shimmer;
}

void mainImage(out vec4 fragColor, in vec2 fragCoord) {
    vec2 uv = fragCoord / iResolution.xy;
    vec4 term = texture(iChannel0, uv);
    float aspect = iResolution.x / iResolution.y;

    float w = webAt(uv, vec2(0.0, 0.0), aspect, SPOOKI_TIME)     // top-left
            + webAt(uv, vec2(1.0, 1.0), aspect, SPOOKI_TIME);    // bottom-right
    vec3 web = vec3(0.82, 0.86, 0.95) * clamp(w, 0.0, 1.0);

    float lum = dot(term.rgb, vec3(0.2126, 0.7152, 0.0722));
    float bgmask = 1.0 - smoothstep(0.05, 0.14, lum);

    vec3 res = term.rgb + web * 0.45 * bgmask;
    fragColor = spooki_out(res, term);
}
