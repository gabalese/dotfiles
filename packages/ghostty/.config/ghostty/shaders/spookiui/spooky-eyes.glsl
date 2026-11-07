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

// SpookiUI treat: Spooky Eyes.
// Pairs of glowing eyes blink open in the dark, linger, then fade and reappear
// somewhere new. Original SpookiUI shader; drawn over dark background pixels.

float se(vec2 p) {
    p = fract(p * vec2(127.1, 311.7));
    p += dot(p, p + 45.32);
    return fract(p.x * p.y);
}

void mainImage(out vec4 fragColor, in vec2 fragCoord) {
    vec2 uv = fragCoord / iResolution.xy;
    vec4 term = texture(iChannel0, uv);
    float aspect = iResolution.x / iResolution.y;

    vec3 acc = vec3(0.0);
    const int N = 5;
    for (int i = 0; i < N; i++) {
        float fi = float(i);
        float period = 5.0 + 3.0 * se(vec2(fi, 1.0));
        float tp = SPOOKI_TIME / period + fi * 0.37;
        float k = floor(tp);
        float f = fract(tp);
        vec2 pos = vec2(0.12 + 0.76 * se(vec2(k, fi + 3.0)),
                        0.15 + 0.70 * se(vec2(k + 7.0, fi)));
        float life = smoothstep(0.0, 0.12, f) * smoothstep(1.0, 0.72, f);
        float blinkAmt = smoothstep(0.44, 0.5, f) * smoothstep(0.56, 0.5, f);
        float rx = 0.017;
        float ry = 0.015 * mix(1.0, 0.12, blinkAmt);           // squash shut on a blink
        vec3 col = mix(vec3(1.0, 0.72, 0.15), vec3(0.45, 1.0, 0.5),
                       step(0.5, se(vec2(fi, 9.0))));          // amber or eerie green
        vec2 d1 = vec2((uv.x - (pos.x - 0.022)) * aspect, uv.y - pos.y);
        vec2 d2 = vec2((uv.x - (pos.x + 0.022)) * aspect, uv.y - pos.y);
        float e1 = smoothstep(1.0, 0.25, length(d1 / vec2(rx, ry)));
        float e2 = smoothstep(1.0, 0.25, length(d2 / vec2(rx, ry)));
        acc += col * (e1 + e2) * life;
    }

    float lum = dot(term.rgb, vec3(0.2126, 0.7152, 0.0722));
    float bgmask = 1.0 - smoothstep(0.05, 0.14, lum);

    vec3 res = term.rgb + acc * 0.7 * bgmask;
    fragColor = spooki_out(res, term);
}
