import os
import sys
import math

try:
    from PIL import Image, ImageDraw, ImageFilter
except ImportError:
    os.system(f"{sys.executable} -m pip install Pillow")
    from PIL import Image, ImageDraw, ImageFilter

def remove_white_bg_and_crop(img, tolerance=15):
    """
    Remove white background and strictly crop to the real character bounds.
    """
    img = img.convert("RGBA")
    r, g, b, a = img.split()
    
    width, height = img.size
    new_alpha = Image.new("L", (width, height))
    pixels_r = r.load()
    pixels_g = g.load()
    pixels_b = b.load()
    pixels_a = a.load()
    pixels_na = new_alpha.load()
    
    for y in range(height):
        for x in range(width):
            pr, pg, pb, pa = pixels_r[x,y], pixels_g[x,y], pixels_b[x,y], pixels_a[x,y]
            if pr > 255-tolerance and pg > 255-tolerance and pb > 255-tolerance:
                pixels_na[x,y] = 0
            else:
                pixels_na[x,y] = pa
                
    new_alpha = new_alpha.filter(ImageFilter.GaussianBlur(0.8))
    img.putalpha(new_alpha)
    
    # Strictly get the bounding box of non-transparent pixels
    bbox = img.getbbox()
    if bbox:
        # Crop exactly to the character
        img = img.crop(bbox)
        
    return img

def create_hd_animated_avatar(input_path, output_path, hd_png_path, size=512):
    print("1. Loading, removing BG, and cropping tightly...")
    img = Image.open(input_path).convert("RGBA")
    img = remove_white_bg_and_crop(img, tolerance=25)
    
    print(f"2. Saving tightly cropped HD transparent PNG to {hd_png_path}...")
    img.save(hd_png_path, "PNG", optimize=True)
    
    # We want the character to take up the maximum possible space in 512x512,
    # leaving just enough padding for the bobbing/scaling logic so it doesn't clip
    padding_needed = size * 0.05 # 5% padding max
    target_canvas_size = size
    
    # Calculate how big the base image should be to fit tightly within the canvas
    w_ratio = (target_canvas_size - padding_needed*2) / img.width
    h_ratio = (target_canvas_size - padding_needed*2) / img.height
    resize_ratio = min(w_ratio, h_ratio)
    
    new_w = int(img.width * resize_ratio)
    new_h = int(img.height * resize_ratio)
    base_img = img.resize((new_w, new_h), Image.Resampling.LANCZOS)
    
    frames = []
    total_frames = 20 
    
    print("3. Generating tightly fitted 512x512 HD GIF Frames...")
    for i in range(total_frames):
        phase = math.sin((i / total_frames) * 2 * math.pi) 
        
        # Keep scaling very tight so it doesn't clip boundaries
        scale = 0.98 + 0.02 * phase
        angle = math.sin((i / total_frames) * 2 * math.pi) * 3.0
        
        # Rotate first
        rotated_base = base_img.rotate(angle, Image.Resampling.BICUBIC, expand=True)
        # Then scale
        scaled_img = rotated_base.resize((int(rotated_base.width * scale), int(rotated_base.height * scale)), Image.Resampling.LANCZOS)
        
        canvas = Image.new("RGBA", (size, size), (0, 0, 0, 0))
        
        # Subtly bob
        bob = int(math.sin((i / total_frames) * 4 * math.pi) * 4)
        
        # Center precisely in 512x512
        offset_x = (size - scaled_img.width) // 2
        offset_y = (size - scaled_img.height) // 2 + bob
        
        canvas.paste(scaled_img, (offset_x, offset_y), scaled_img)
        
        # Blinking relative logic
        if i in [14, 15, 16]:
            draw = ImageDraw.Draw(canvas)
            lx = offset_x + scaled_img.width * 0.35 + (angle * 0.5)
            rx = offset_x + scaled_img.width * 0.65 + (angle * 0.5)
            ey = offset_y + scaled_img.height * 0.44
            ew = scaled_img.width * 0.15
            
            draw.arc([lx - ew/2, ey - ew/4, lx + ew/2, ey + ew/4], 200, 340, fill=(35,20,10,255), width=8)
            draw.arc([rx - ew/2, ey - ew/4, rx + ew/2, ey + ew/4], 200, 340, fill=(35,20,10,255), width=8)
            
        alpha = canvas.split()[3]
        quantized = canvas.convert("P", palette=Image.Palette.ADAPTIVE, colors=255)
        mask = Image.eval(alpha, lambda a: 255 if a <= 128 else 0)
        quantized.paste(255, mask)
        quantized.info['transparency'] = 255

        frames.append(quantized)

    print("4. Saving tight 512x512 GIF...")
    frames[0].save(
        output_path, 
        save_all=True, 
        append_images=frames[1:], 
        optimize=False, 
        duration=1000//15, 
        loop=0,
        disposal=2
    )
    
    kb = os.path.getsize(output_path) / 1024
    print(f"Created True HD animated avatar at: {output_path} ({kb:.1f} KB)")


if __name__ == "__main__":
    create_hd_animated_avatar(
        "/Users/huangzhonghui/HotPlex/docs/images/hotplex_beaver_cute_base.png", 
        "/Users/huangzhonghui/HotPlex/docs/images/hotplex_avatar_512x512_tight.gif",
        "/Users/huangzhonghui/HotPlex/docs/images/hotplex_beaver_cutout_tight.png",
        size=512
    )
