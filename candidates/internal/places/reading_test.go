package places_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// R-WMSL-Y98E
func TestAddress(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  [4]string
	}{
		{"17 Example Rd, Testville, MN 55123, USA", [4]string{"17 Example Rd", "Testville", "MN", "55123"}},
		{"17 Example Rd, Testville, MN 55123, United States", [4]string{"17 Example Rd", "Testville", "MN", "55123"}},
		{" 17 Example Rd , , Suite 4 , Testville , MN 55123-4567 , USA , ", [4]string{"17 Example Rd, Suite 4", "Testville", "MN", "55123"}},
		{"Testville, MN 55123, USA", [4]string{"", "Testville", "MN", "55123"}},
		{"17 Example Rd, Testville, MN 55123", [4]string{"17 Example Rd", "Testville", "MN", "55123"}},
		{"  Testville, Minnesota  ", [4]string{"Testville, Minnesota", "", "", ""}},
		{"Somewhere", [4]string{"Somewhere", "", "", ""}}, {"", [4]string{}},
		{"MN 55123, USA", [4]string{"MN 55123, USA", "", "", ""}},
		{"17 Example Rd, Testville, MN 55123, usa", [4]string{"17 Example Rd, Testville, MN 55123, usa", "", "", ""}},
	} {
		street, city, state, zip := places.Address(tt.input)
		got := [4]string{street, city, state, zip}
		if got != tt.want {
			t.Errorf("address %q=%q want %q", tt.input, got, tt.want)
		}
	}
	for _, suffix := range []string{"mn 55123", "M 55123", "MNN 55123", "MN  55123", "MN\t55123", "MN 5512", "MN 551234", "MN 55123-123", "MN 55123-12345", "MN ５５１２３", "ÄN 55123"} {
		input := "Testville, " + suffix
		street, city, state, zip := places.Address(input)
		if street != input || city != "" || state != "" || zip != "" {
			t.Errorf("accepted suffix %q", suffix)
		}
	}
}

// R-NYSO-MYYP
func TestDomainKey(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"https://www.Example.test/about", "example.test"}, {"http://shop.example.test:8080/", "shop.example.test"},
		{"https://notfacebook.com/", "notfacebook.com"}, {"https://www.www.example.test/", "www.example.test"},
		{"https://gmail.com/", ""}, {"https://yahoo.com/", ""}, {"not a url", ""}, {"", ""}, {"/relative/path", ""}, {"https://%invalid/", ""},
	} {
		if got := places.DomainKey(tt.in); got != tt.want {
			t.Errorf("domain %q=%q want %q", tt.in, got, tt.want)
		}
	}
	hosts := strings.Fields("facebook.com fb.com instagram.com linkedin.com twitter.com x.com youtube.com tiktok.com pinterest.com linktr.ee google.com goo.gl business.site wixsite.com weebly.com squarespace.com godaddysites.com wordpress.com blogspot.com square.site carrd.co yelp.com yellowpages.com bbb.org mapquest.com nextdoor.com angi.com homeadvisor.com houzz.com thumbtack.com manta.com chamberofcommerce.com etsy.com amazon.com ebay.com")
	for _, host := range hosts {
		for _, prefix := range []string{"", "www.", "tenant."} {
			u := "https://" + prefix + host + "/shop"
			if got := places.DomainKey(u); got != "" {
				t.Errorf("shared host %q=%q", u, got)
			}
		}
	}
}

// R-0GCB-QVFN
func TestCategoryTable(t *testing.T) {
	groups := []struct{ category, types string }{
		{sources.CategoryConstruction, "general_contractor electrician plumber roofing_contractor painter"},
		{sources.CategoryTrucking, "moving_company courier_service storage truck_stop"},
		{sources.CategoryAgriculture, "farm ranch"},
		{sources.CategoryManufacturing, "manufacturer"},
		{sources.CategoryWholesale, "wholesaler"},
		{sources.CategoryUtilities, "electric_utility"},
		{sources.CategoryRetail, "store hardware_store home_improvement_store furniture_store home_goods_store electronics_store clothing_store convenience_store department_store supermarket grocery_store liquor_store pet_store shoe_store sporting_goods_store jewelry_store book_store bicycle_store auto_parts_store cell_phone_store discount_store gift_shop shopping_mall warehouse_store florist car_dealer gas_station pharmacy drugstore"},
		{sources.CategoryOtherTransportation, "airport bus_station train_station transit_station taxi_stand car_rental"},
		{sources.CategoryInformation, "telecommunications_service_provider"},
		{sources.CategoryFinance, "bank atm insurance_agency"},
		{sources.CategoryRealEstate, "real_estate_agency"},
		{sources.CategoryProfessional, "accounting lawyer consultant veterinary_care"},
		{sources.CategoryAdministrative, "travel_agency tour_agency"},
		{sources.CategoryEducation, "school primary_school secondary_school university preschool"},
		{sources.CategoryHealthCare, "hospital doctor dentist physiotherapist"},
		{sources.CategoryArts, "gym museum art_gallery golf_course bowling_alley amusement_park"},
		{sources.CategoryFood, "restaurant cafe bar bakery meal_takeaway meal_delivery fast_food_restaurant lodging hotel motel campground rv_park"},
		{sources.CategoryRepair, "car_repair car_wash hair_care hair_salon beauty_salon barber_shop laundry locksmith funeral_home nail_salon"},
		{sources.CategoryReligious, "church place_of_worship mosque synagogue hindu_temple"},
		{sources.CategoryGovernment, "city_hall courthouse police fire_station post_office local_government_office embassy"},
	}
	for _, group := range groups {
		for _, typ := range strings.Fields(group.types) {
			if got := places.Category(typ); got != group.category {
				t.Errorf("category %q=%q want %q", typ, got, group.category)
			}
			for _, bad := range []string{strings.ToUpper(typ), " " + typ, typ + " ", "unknown_" + typ} {
				if places.Category(bad) != "" {
					t.Errorf("nonexact type %q mapped", bad)
				}
			}
		}
	}
	for _, typ := range []string{"", "establishment", "point_of_interest", "mining", "waste", "other", "unknown"} {
		if got := places.Category(typ); got != "" {
			t.Errorf("unknown category %q=%q", typ, got)
		}
	}
}

// R-O00L-0QPE
func TestReading(t *testing.T) {
	for _, p := range []places.Place{
		{ID: "place-own", Name: "Sample Fabricators LLC", Address: "17 Example Rd, Testville, MN 55123, USA", Phone: "+1 (218) 555-0123", Website: "https://www.fabricators.test/", PrimaryType: "manufacturer"},
		{ID: "city-own", Name: "City of Testville", Address: "Testville, MN 55123, USA", Phone: "555-0123", Website: "https://facebook.com/testville", PrimaryType: "city_hall"},
		{},
	} {
		names := []string{}
		if p.Name != "" {
			names = append(names, p.Name)
		}
		expected := sources.Reading{Name: p.Name, Names: names, NameKey: sources.NameKey(p.Name), NameKeys: sources.NameKeys(names), People: []sources.Person{}, Cargo: []string{}, Evidence: []sources.Evidence{}, FleetVerdict: sources.FleetUnknown}
		switch p.ID {
		case "place-own":
			expected.Street = "17 Example Rd"
			expected.City = "Testville"
			expected.State = "MN"
			expected.Zip = "55123"
			expected.Phone = "2185550123"
			expected.PhoneKey = "2185550123"
			expected.DomainKey = "fabricators.test"
			expected.Category = sources.CategoryManufacturing
		case "city-own":
			expected.City = "Testville"
			expected.State = "MN"
			expected.Zip = "55123"
			expected.Category = sources.CategoryGovernment
		}
		if got := places.Reading(p); !reflect.DeepEqual(got, expected) {
			t.Errorf("reading = %#v want %#v", got, expected)
		}
	}
}
