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

// SpookiUI treat: Pipes.
// A neon 2D homage to the Windows 95 "3D Pipes" screensaver: a lattice of
// glowing pipe segments that pulse and cycle colour. A true 3D pipes maze needs
// real geometry a terminal shader can't do, so this evokes the vibe in 2D.
// Original SpookiUI shader; drawn only over dark background pixels.

float h2(vec2 p) {
    p = fract(p * vec2(127.1, 311.7));
    p += dot(p, p + 34.1);
    return fract(p.x * p.y);
}

float seg(vec2 p, vec2 a, vec2 b) {
    vec2 pa = p - a, ba = b - a;
    float t = clamp(dot(pa, ba) / dot(ba, ba), 0.0, 1.0);
    return length(pa - ba * t);
}

void mainImage(out vec4 fragColor, in vec2 fragCoord) {
    vec2 uv = fragCoord / iResolution.xy;
    vec4 term = texture(iChannel0, uv);

    float s = iResolution.y / 9.0;      // grid cell size (px) -> ~9 rows
    vec2 g = fragCoord / s;             // grid-space coords (cell units)
    vec2 cell = floor(g);

    float best = 1e9;
    vec2 bestNode = vec2(0.0);
    // Test the pipe segments radiating from the 3x3 nearest lattice nodes.
    for (int oy = -1; oy <= 1; oy++) {
        for (int ox = -1; ox <= 1; ox++) {
            vec2 node = cell + vec2(float(ox), float(oy));
            if (h2(node + vec2(0.3, 0.7)) > 0.45) {          // edge going right
                float d = seg(g, node, node + vec2(1.0, 0.0));
                if (d < best) { best = d; bestNode = node + vec2(0.5, 0.0); }
            }
            if (h2(node + vec2(0.8, 0.2)) > 0.45) {          // edge going up
                float d = seg(g, node, node + vec2(0.0, 1.0));
                if (d < best) { best = d; bestNode = node + vec2(0.0, 0.5); }
            }
        }
    }

    float pipeR = 0.16;                 // pipe radius in cell units
    float core = 1.0 - smoothstep(pipeR * 0.5, pipeR, best);
    float glow = 1.0 - smoothstep(pipeR, pipeR * 2.6, best);

    float hue = fract(0.05 * SPOOKI_TIME + 0.15 * (bestNode.x + bestNode.y));
    vec3 col = 0.5 + 0.5 * cos(6.2831 * (hue + vec3(0.0, 0.33, 0.67)));
    float pulse = 0.6 + 0.4 * sin(SPOOKI_TIME * 3.0 + (bestNode.x + bestNode.y) * 1.7);
    vec3 pipe = col * (core * 0.55 + glow * 0.22) * pulse;

    float lum = dot(term.rgb, vec3(0.2126, 0.7152, 0.0722));
    float bgmask = 1.0 - smoothstep(0.05, 0.15, lum);

    vec3 res = term.rgb + pipe * bgmask;
    fragColor = spooki_out(res, term);
}
